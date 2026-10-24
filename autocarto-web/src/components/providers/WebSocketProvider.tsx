"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import wsClient from "@/lib/websocket";
import api, {
  getApiErrorMessage,
  normalizeRuntimeSnapshot,
  type RuntimeSnapshot,
} from "@/lib/api";
import {
  normalizeWorkflowToolStatus,
  reconcileWorkflowBlocksWithTaskListUpdate,
  shouldPreserveTaskListUpdateCompletion,
  toTodoItems,
  toWorkflowBlocks,
} from "@/lib/chat-message-utils";
import { resolveExecutionMode } from "@/lib/runIntent";
import { loadSessionsForProject } from "@/lib/session-utils";
import {
  getGlobalPersistenceController,
  useGlobalPersistence,
} from "@/hooks/useGlobalPersistence";
import { useHydration } from "@/hooks/useHydration";
import {
  createInitialRunEventSeqState,
  normalizeRunEventSeqSessionId,
  removeRunEventSeqState,
  upsertRunEventSeqState,
} from "@/hooks/runEventSeqState";
import { initUIStoreBridge } from "@/lib/uiStoreBridge";
import {
  HEALTH_CHECK_INTERVAL_MS,
  useConnectionStore,
} from "@/stores/connectionStore";
import { useChatStore } from "@/stores/chatStore";
import { useSessionStore } from "@/stores/sessionStore";
import { useInstanceStore } from "@/stores/instanceStore";
import { useRuntimeSnapshotStore } from "@/stores/runtimeSnapshotStore";
import {
  AutoPhase,
  ContextResponseData,
  InterruptRequestData,
  Message,
  TodoItem,
  WorkflowBlock,
  WorkflowEntry,
  WorkflowToolCard,
  WSMessage,
} from "@/types/message";
import { generateId } from "@/lib/utils";
import { parseReviewReference } from "@/features/knowledge-review/model";
import { useFlowRunStore } from "@/features/flow-run/store";
import { terminalFlow } from "@/features/flow-run/model";
import { parseFlowPreview } from "@/features/flow-run/FlowPreviewCard";
import type { FlowEvent } from "@/features/flow-run/types";
import {
  collectSessionAttachTargets,
  markSessionAttachStateLive,
  markSessionAttachStateReplayRequired,
  markSessionAttachStatesReattaching,
  pruneSessionAttachStates,
  resolveSessionAttachAckFromNormalizedMessage,
  resolveSessionAttachBlockFromNormalizedMessage,
  resolveSessionAttachAckFromRawMessage,
  type SessionAttachStatus,
} from "@/lib/sessionReattach";
import { isDesktopRuntime } from "@/lib/tauri";

function asRecord(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object") {
    return {};
  }
  return value as Record<string, unknown>;
}

function normalizeRevisionToken(value: unknown) {
  return typeof value === "string" ? value.trim() : "";
}

function runtimeSnapshotRevisionSignature(
  snapshot?: Partial<RuntimeSnapshot> | null,
) {
  if (!snapshot) {
    return "";
  }
  const toolRevisions = snapshot.tool_revisions || {};
  const signature = {
    config_revision: normalizeRevisionToken(
      snapshot.config_revision || snapshot.runtime_revision,
    ),
    discovery_revision: normalizeRevisionToken(
      snapshot.discovery_revision || snapshot.runtime_revision,
    ),
    runtime_revision: normalizeRevisionToken(snapshot.runtime_revision),
    tool_catalog_revision: normalizeRevisionToken(
      snapshot.tool_catalog_revision || toolRevisions.catalog,
    ),
    tool_schema_revision: normalizeRevisionToken(
      snapshot.tool_schema_revision || toolRevisions.schema,
    ),
    tool_availability_revision: normalizeRevisionToken(
      snapshot.tool_availability_revision || toolRevisions.availability,
    ),
  };
  if (Object.values(signature).every((value) => !value)) {
    return "";
  }
  return JSON.stringify(signature);
}

function shouldForceRefreshRuntimeSnapshot(
  currentSnapshot: RuntimeSnapshot,
  incomingSnapshot?: Partial<RuntimeSnapshot> | null,
) {
  const normalizedIncoming = normalizeRuntimeSnapshot(incomingSnapshot);
  if (normalizedIncoming.stale) {
    return true;
  }
  const currentSignature = runtimeSnapshotRevisionSignature(currentSnapshot);
  const incomingSignature = runtimeSnapshotRevisionSignature(normalizedIncoming);
  return Boolean(
    currentSignature &&
      incomingSignature &&
      currentSignature !== incomingSignature,
  );
}

const unsupportedContractCodes = new Set([
  "UNSUPPORTED_PROTOCOL_VERSION",
  "HTTP_CONTRACT_VERSION_UNSUPPORTED",
]);

function resolveUnsupportedContractCode(msg: WSMessage) {
  const data = asRecord(msg.data);
  const code = String(data.code || "").trim().toUpperCase();
  if (unsupportedContractCodes.has(code)) {
    return code;
  }
  return "";
}

function resolveUnsupportedContractMessage(msg: WSMessage) {
  const data = asRecord(msg.data);
  const userAction = String(data.user_action || "").trim();
  if (userAction) {
    return userAction;
  }
  const message = String(data.message || msg.content || "").trim();
  if (message) {
    return message;
  }
  return "检测到客户端与后端协议版本不兼容，请刷新或升级客户端后重试。";
}

function isStickyUnsupportedContract(detail: string) {
  const normalized = detail.trim();
  if (!normalized) {
    return false;
  }
  return normalized.includes("协议版本") || normalized.includes("升级客户端");
}

function normalizeSessionId(value: unknown) {
  return normalizeRunEventSeqSessionId(value);
}

function areSessionAttachStatesEqual(
  left: Record<string, SessionAttachStatus>,
  right: Record<string, SessionAttachStatus>,
) {
  const leftKeys = Object.keys(left);
  const rightKeys = Object.keys(right);
  if (leftKeys.length !== rightKeys.length) {
    return false;
  }
  return leftKeys.every((key) => left[key] === right[key]);
}

function parseTodos(msg: WSMessage) {
  const data = asRecord(msg.data);
  const todos = Array.isArray(data.todos)
    ? data.todos
    : Array.isArray(data.tasks)
      ? data.tasks
      : Array.isArray(data.items)
        ? data.items
        : [];
  if (todos.length === 0) {
    return [];
  }
  return todos
    .filter((item) => item && typeof item === "object")
    .map((item) => {
      const payload = item as Record<string, unknown>;
      return {
        content:
          typeof payload.content === "string"
            ? payload.content
            : String(payload.content || payload.title || payload.name || ""),
        status:
          typeof payload.status === "string"
            ? payload.status
            : String(payload.status || ""),
        priority:
          typeof payload.priority === "string" ? payload.priority : undefined,
        owner: typeof payload.owner === "string" ? payload.owner : undefined,
      } satisfies TodoItem;
    });
}

function normalizeIncomingMessage(msg: WSMessage): WSMessage {
  if (msg.type !== "run_event") {
    return msg;
  }

  const data = asRecord(msg.data);
  const payload = asRecord(data.payload);

  return {
    ...msg,
    session_id: normalizeSessionId(payload.session_id) || msg.session_id,
  };
}

const sessionScopedMessageTypes = new Set([
  "run_event",
  "thinking",
  "response",
  "error",
  "cancelled",
  "task_list_update",
  "interrupt_request",
  "context.response",
  "decision",
  "status",
  "agent_workflow",
  "mode_state",
  "phase_transition",
  "tool",
  "agent",
  "task_running_changed",
  "flow_event",
]);

function parseAutoPhase(raw: unknown): AutoPhase {
  const phase = String(raw || "").toLowerCase();
  if (
    phase === "planning" ||
    phase === "evaluating" ||
    phase === "building" ||
    phase === "completed"
  ) {
    return phase;
  }
  return "idle";
}

function buildWorkflowSummary(content: string, phase: string) {
  const text = content.trim();
  if (text) {
    return text;
  }
  if (phase === "tool_call") {
    return "正在调用工具处理任务";
  }
  if (phase === "tool_result") {
    return "正在整理工具返回结果";
  }
  if (phase === "decision") {
    return "正在形成执行决策";
  }
  return "正在处理任务";
}

function createWorkflowBlock(msg: WSMessage, timestamp: number): WorkflowBlock {
  const phase = String(msg.phase || "thinking").toLowerCase();
  const agent = String(msg.agent || "orchestrator");
  const displayName = String(msg.agent_display_name || msg.agent || "智能体");
  return {
    id: `wf-${timestamp}-${generateId()}`,
    agent,
    agentDisplayName: displayName,
    summary: buildWorkflowSummary(String(msg.content || ""), phase),
    status: "running",
    startedAt: timestamp,
    updatedAt: timestamp,
    entries: [],
  };
}

function createWorkflowEntry(
  msg: WSMessage,
  timestamp: number,
): WorkflowEntry | null {
  const phase = String(msg.phase || "thinking").toLowerCase();
  const content = String(msg.content || "");
  const data = asRecord(msg.data);

  if (phase === "tool_call" || phase === "tool_result") {
    const tool = String(data.tool || "未知工具");
    const toolCard: WorkflowToolCard = {
      id: `tool-${timestamp}-${generateId()}`,
      tool,
      status:
        phase === "tool_call"
          ? "running"
          : normalizeWorkflowToolStatus(data.status, data.success),
      args: asRecord(data.tool_args),
      result: String(data.result || content || ""),
      statusSource: String(data.status_source || ""),
      success: typeof data.success === "boolean" ? data.success : undefined,
      startedAt: timestamp,
      finishedAt: phase === "tool_result" ? timestamp : undefined,
    };

    return {
      id: `entry-${timestamp}-${generateId()}`,
      type: "tool",
      content:
        content ||
        (phase === "tool_call" ? `调用工具: ${tool}` : `工具返回: ${tool}`),
      phase,
      timestamp,
      data,
      toolCard,
    };
  }

  if (!content) {
    return null;
  }

  if (phase === "decision") {
    return {
      id: `entry-${timestamp}-${generateId()}`,
      type: "decision",
      content,
      phase,
      timestamp,
      data,
    };
  }

  if (phase === "thinking") {
    return {
      id: `entry-${timestamp}-${generateId()}`,
      type: "thinking",
      content,
      phase,
      timestamp,
      data,
    };
  }

  return {
    id: `entry-${timestamp}-${generateId()}`,
    type: "status",
    content,
    phase,
    timestamp,
    data,
  };
}

function completeRunningBlock(
  blocks: WorkflowBlock[],
  status: "completed" | "error",
  timestamp: number,
  blockId: string | null,
) {
  if (!blockId) {
    return blocks;
  }
  const index = blocks.findIndex((item) => item.id === blockId);
  if (index < 0) {
    return blocks;
  }
  const target = blocks[index];
  if (target.status !== "running") {
    return blocks;
  }
  const next = [...blocks];
  next[index] = {
    ...target,
    status,
    updatedAt: timestamp,
    finishedAt: timestamp,
  };
  return next;
}

function appendOrMergeWorkflowEntry(
  block: WorkflowBlock,
  entry: WorkflowEntry,
  timestamp: number,
): WorkflowBlock {
  if (entry.phase === "progress" && entry.data?.request_key) {
    const index = block.entries.findIndex(
      (item) => item.phase === "progress" &&
        item.data?.request_key === entry.data?.request_key,
    );
    if (index >= 0) {
      const entries = [...block.entries];
      entries[index] = { ...entry, id: entries[index].id };
      return { ...block, entries, updatedAt: timestamp };
    }
  }
  if (!entry.toolCard) {
    return {
      ...block,
      entries: [...block.entries, entry],
      updatedAt: timestamp,
    };
  }

  if (entry.phase === "tool_result") {
    const tool = entry.toolCard.tool;
    const entryStatus = entry.toolCard.status;
    const index = [...block.entries]
      .reverse()
      .findIndex(
        (item) =>
          item.toolCard &&
          item.toolCard.tool === tool &&
          (item.toolCard.status === "running" ||
            shouldPreserveTaskListUpdateCompletion(item.toolCard, entry.toolCard)),
      );

    if (index >= 0) {
      const targetIndex = block.entries.length - 1 - index;
      const target = block.entries[targetIndex];
      const preserveTaskListCompletion = shouldPreserveTaskListUpdateCompletion(
        target.toolCard,
        entry.toolCard,
      );
      const merged: WorkflowEntry = {
        ...target,
        content: preserveTaskListCompletion
          ? target.content
          : entry.content || target.content,
        phase: "tool_result",
        timestamp,
        data: {
          ...(target.data || {}),
          ...(entry.data || {}),
        },
        toolCard: {
          ...target.toolCard!,
          status: preserveTaskListCompletion ? "completed" : entryStatus,
          result: preserveTaskListCompletion
            ? target.toolCard?.result || entry.toolCard.result
            : entry.toolCard.result || target.toolCard?.result,
          statusSource:
            preserveTaskListCompletion
              ? target.toolCard?.statusSource || "task_list_update"
              : entry.toolCard.statusSource || target.toolCard?.statusSource,
          success:
            preserveTaskListCompletion
              ? true
              : typeof entry.toolCard.success === "boolean"
              ? entry.toolCard.success
              : target.toolCard?.success,
          finishedAt: preserveTaskListCompletion
            ? target.toolCard?.finishedAt || timestamp
            : timestamp,
        },
      };
      const nextEntries = [...block.entries];
      nextEntries[targetIndex] = merged;
      return {
        ...block,
        entries: nextEntries,
        updatedAt: timestamp,
      };
    }
  }

  return {
    ...block,
    entries: [...block.entries, entry],
    updatedAt: timestamp,
  };
}

function isActiveSessionMessage(
  sessionId: string,
  activeSessionId: string | null,
) {
  if (!sessionId) {
    return true;
  }
  if (!activeSessionId) {
    return false;
  }
  return sessionId === activeSessionId;
}

export default function WebSocketProvider({
  children,
}: {
  children: React.ReactNode;
}) {
  const initializeRuntime = useConnectionStore((s) => s.initializeRuntime);
  const waitForBackend = useConnectionStore((s) => s.waitForBackend);
  const checkHealth = useConnectionStore((s) => s.checkHealth);
  const backendReady = useConnectionStore((s) => s.backendReady);
  const backendUrl = useConnectionStore((s) => s.backendUrl);
  const wsUrl = useConnectionStore((s) => s.wsUrl);
  const startupError = useConnectionStore((s) => s.startupError);
  const setConnected = useConnectionStore((s) => s.setConnected);

  const addMessage = useChatStore((s) => s.addMessage);
  const addSessionMessage = useChatStore((s) => s.addSessionMessage);
  const setSessionThinking = useChatStore((s) => s.setSessionThinking);
  const setSessionRunning = useChatStore((s) => s.setSessionRunning);
  const setSessionRuntimeState = useChatStore((s) => s.setSessionRuntimeState);
  const setSessionTasks = useChatStore((s) => s.setSessionTasks);
  const addSessionInterrupt = useChatStore((s) => s.addSessionInterrupt);
  const clearSessionInterrupts = useChatStore((s) => s.clearSessionInterrupts);
  const setContextStats = useChatStore((s) => s.setContextStats);
  const setLastContextResponse = useChatStore((s) => s.setLastContextResponse);
  const setExecutionMode = useChatStore((s) => s.setExecutionMode);
  const setAutoPhase = useChatStore((s) => s.setAutoPhase);
  const setActiveModelName = useChatStore((s) => s.setActiveModelName);
  const setActiveAgentConfig = useChatStore((s) => s.setActiveAgentConfig);
  const setAvailableModels = useChatStore((s) => s.setAvailableModels);
  const setAvailableAgentConfigs = useChatStore(
    (s) => s.setAvailableAgentConfigs,
  );
  const setSessionWorkflowBlocks = useChatStore(
    (s) => s.setSessionWorkflowBlocks,
  );
  const setSessionActiveWorkflowBlockId = useChatStore(
    (s) => s.setSessionActiveWorkflowBlockId,
  );
  const setSessionFocusedWorkflowBlockId = useChatStore(
    (s) => s.setSessionFocusedWorkflowBlockId,
  );
  const purgeSession = useChatStore((s) => s.purgeSession);
  const chatSessionId = useChatStore((s) => s.sessionId);
  const runningSessionIds = useChatStore((s) => s.runningSessionIds);

  const setSessions = useSessionStore((s) => s.setSessions);
  const upsertSession = useSessionStore((s) => s.upsertSession);
  const removeSession = useSessionStore((s) => s.removeSession);
  const activeSessionId = useSessionStore((s) => s.activeSessionId);
  const setSessionAttachStates = useSessionStore(
    (s) => s.setSessionAttachStates,
  );
  const instances = useInstanceStore((s) => s.instances);
  const activeInstanceId = useInstanceStore((s) => s.activeInstanceId);
  const fetchRuntimeSnapshot = useRuntimeSnapshotStore((s) => s.fetchSnapshot);
  const applyRuntimeSnapshot = useRuntimeSnapshotStore((s) => s.applySnapshot);

  const [checking, setChecking] = useState(true);
  const [failed, setFailed] = useState(false);
  const [retryToken, setRetryToken] = useState(0);
  const wasConnectedRef = useRef(false);
  const hasEverLoadedRef = useRef(false);
  const realtimeMessageHandlerRef = useRef<(msg: WSMessage) => void>(() => {});
  const refreshSessionsRef = useRef<() => Promise<void>>(async () => {});
  const syncSessionAttachmentsRef = useRef<() => void>(() => {});
  const attachedSessionsRef = useRef<Set<string>>(new Set());
  const runEventSeqRef = useRef<Record<string, number>>(
    createInitialRunEventSeqState(),
  );
  const reattachPendingRef = useRef(false);

  const activeProjectPath = useMemo(() => {
    const activeInstance = instances.find(
      (item) => item.id === activeInstanceId,
    );
    return activeInstance?.project_path || "";
  }, [activeInstanceId, instances]);

  useGlobalPersistence(activeProjectPath);
  useHydration(backendReady);

  useEffect(() => {
    initUIStoreBridge();
  }, []);

  useEffect(() => {
    api.setBaseURL(backendUrl);
    wsClient.setUrl(wsUrl);
  }, [backendUrl, wsUrl]);

  const refreshSessions = useCallback(async () => {
    try { const list = await loadSessionsForProject(
      (path) => api.getSessions(path),
      activeProjectPath,
    )
    setSessions(list) } catch (error) { void error;  }
  }, [activeProjectPath, setSessions]);

  const appendWorkflowEvent = useCallback(
    (sessionId: string, workflowMsg: WSMessage) => {
      if (!sessionId) {
        return;
      }

      const timestamp = Date.now();
      const phase = String(workflowMsg.phase || "thinking").toLowerCase();
      const state = useChatStore.getState();
      let blocks = [...(state.sessionWorkflowBlocks[sessionId] || [])];
      let activeBlockId = state.sessionActiveWorkflowBlockId[sessionId] || null;
      let focusedBlockId =
        state.sessionFocusedWorkflowBlockId[sessionId] || null;

      let activeIndex = activeBlockId
        ? blocks.findIndex((item) => item.id === activeBlockId)
        : -1;
      let activeBlock = activeIndex >= 0 ? blocks[activeIndex] : null;

      const incomingAgent = String(workflowMsg.agent || "orchestrator");
      if (
        !activeBlock ||
        activeBlock.status !== "running" ||
        activeBlock.agent !== incomingAgent
      ) {
        if (
          focusedBlockId &&
          activeBlockId &&
          focusedBlockId === activeBlockId
        ) {
          focusedBlockId = null;
          setSessionFocusedWorkflowBlockId(sessionId, null);
        }
        blocks = completeRunningBlock(
          blocks,
          "completed",
          timestamp,
          activeBlockId,
        );
        const nextBlock = createWorkflowBlock(workflowMsg, timestamp);
        blocks = [...blocks, nextBlock];
        activeBlockId = nextBlock.id;
        activeIndex = blocks.length - 1;
        activeBlock = nextBlock;
      }

      if (!activeBlock || activeIndex < 0) {
        setSessionWorkflowBlocks(sessionId, blocks);
        setSessionActiveWorkflowBlockId(sessionId, activeBlockId);
        return;
      }

      let updatedBlock = activeBlock;
      if (
        phase === "thinking" ||
        phase === "decision" ||
        phase === "tool_call" ||
        phase === "progress"
      ) {
        updatedBlock = {
          ...updatedBlock,
          summary: buildWorkflowSummary(
            String(workflowMsg.content || ""),
            phase,
          ),
        };
      }

      const entry = createWorkflowEntry(workflowMsg, timestamp);
      if (entry) {
        updatedBlock = appendOrMergeWorkflowEntry(
          updatedBlock,
          entry,
          timestamp,
        );
      } else {
        updatedBlock = {
          ...updatedBlock,
          updatedAt: timestamp,
        };
      }

      blocks = [...blocks];
      blocks[activeIndex] = updatedBlock;
      setSessionWorkflowBlocks(sessionId, blocks);
      setSessionActiveWorkflowBlockId(sessionId, activeBlockId);

      if (!focusedBlockId && activeBlockId) {
        setSessionFocusedWorkflowBlockId(sessionId, activeBlockId);
      }
    },
    [
      setSessionActiveWorkflowBlockId,
      setSessionFocusedWorkflowBlockId,
      setSessionWorkflowBlocks,
    ],
  );

  const completeSessionWorkflowBlock = useCallback(
    (sessionId: string, status: "completed" | "error") => {
      if (!sessionId) {
        return;
      }
      const state = useChatStore.getState();
      const timestamp = Date.now();
      const blocks = state.sessionWorkflowBlocks[sessionId] || [];
      const activeBlockId =
        state.sessionActiveWorkflowBlockId[sessionId] || null;
      const focusedBlockId =
        state.sessionFocusedWorkflowBlockId[sessionId] || null;
      const next = completeRunningBlock(
        blocks,
        status,
        timestamp,
        activeBlockId,
      );
      setSessionWorkflowBlocks(sessionId, next);
      setSessionActiveWorkflowBlockId(sessionId, null);
      if (focusedBlockId && activeBlockId && focusedBlockId === activeBlockId) {
        setSessionFocusedWorkflowBlockId(sessionId, null);
      }
    },
    [
      setSessionActiveWorkflowBlockId,
      setSessionFocusedWorkflowBlockId,
      setSessionWorkflowBlocks,
    ],
  );

  const appendSessionAwareMessage = useCallback(
    (
      sessionId: string,
      activeMessage: boolean,
      payload: Omit<Message, "id" | "timestamp">,
    ) => {
      if (activeMessage) {
        addMessage(payload);
        return;
      }
      if (sessionId) {
        addSessionMessage(sessionId, payload);
      }
    },
    [addMessage, addSessionMessage],
  );

  const upsertRunEventSeq = useCallback((sessionId: string, seq: number) => {
    const result = upsertRunEventSeqState(runEventSeqRef.current, sessionId, seq);
    runEventSeqRef.current = result.nextState;
    return result.changed;
  }, []);

  const removeRunEventSeq = useCallback((sessionId: string) => {
    runEventSeqRef.current = removeRunEventSeqState(runEventSeqRef.current, sessionId);
  }, []);

  const commitSessionAttachStates = useCallback(
    (nextSessionAttachState: Record<string, SessionAttachStatus>) => {
      const currentSessionAttachState =
        useSessionStore.getState().sessionAttachState;
      if (
        areSessionAttachStatesEqual(
          currentSessionAttachState,
          nextSessionAttachState,
        )
      ) {
        return;
      }
      setSessionAttachStates(nextSessionAttachState);
    },
    [setSessionAttachStates],
  );

  const promoteSessionAttachStateLive = useCallback(
    (sessionId: string) => {
      const normalizedSessionId = normalizeSessionId(sessionId);
      if (!normalizedSessionId) {
        return;
      }
      const currentSessionAttachState =
        useSessionStore.getState().sessionAttachState;
      if (
        !(normalizedSessionId in currentSessionAttachState) ||
        currentSessionAttachState[normalizedSessionId] === "live"
      ) {
        return;
      }
      commitSessionAttachStates(
        markSessionAttachStateLive(
          currentSessionAttachState,
          normalizedSessionId,
        ),
      );
    },
    [commitSessionAttachStates],
  );

  const syncSessionAttachments = useCallback(() => {
    const desiredSessionIds = collectSessionAttachTargets({
      activeSessionId,
      chatSessionId,
      runningSessionIds,
    });
    const desired = new Set<string>(desiredSessionIds);

    if (!wsClient.connected) {
      if (reattachPendingRef.current) {
        commitSessionAttachStates(
          markSessionAttachStatesReattaching(
            useSessionStore.getState().sessionAttachState,
            desiredSessionIds,
          ),
        );
      }
      return;
    }

    commitSessionAttachStates(
      pruneSessionAttachStates(
        useSessionStore.getState().sessionAttachState,
        desiredSessionIds,
      ),
    );

    attachedSessionsRef.current.forEach((sessionId) => {
      if (!desired.has(sessionId)) {
        wsClient.sendSessionDetach(sessionId);
      }
    });

    desired.forEach((sessionId) => {
      if (!attachedSessionsRef.current.has(sessionId)) {
        wsClient.sendSessionAttach(
          sessionId,
          runEventSeqRef.current[sessionId],
        );
      }
    });

    attachedSessionsRef.current = desired;
  }, [
    activeSessionId,
    chatSessionId,
    commitSessionAttachStates,
    runningSessionIds,
  ]);

  const handleRealtimeMessage = useCallback(
    (msg: WSMessage) => {
      if (msg.type === "run_event") {
        const attachAckSessionId = resolveSessionAttachAckFromRawMessage(msg);
        const eventData = asRecord(msg.data);
        const payload = asRecord(eventData.payload);
        const replaySessionId =
          normalizeSessionId(payload.session_id) ||
          normalizeSessionId(msg.session_id);
        const replaySeq = Number(eventData.seq || 0);
        if (replaySessionId && Number.isFinite(replaySeq) && replaySeq > 0) {
          const accepted = upsertRunEventSeq(replaySessionId, replaySeq);
          if (!accepted) {
            return;
          }
          if (attachAckSessionId) {
            promoteSessionAttachStateLive(attachAckSessionId);
          }
        }
        const source = String(eventData.source_type || "");
        if (source === "session_state" && Object.prototype.hasOwnProperty.call(payload, "flow_run")) {
          const sid = normalizeSessionId(payload.session_id || msg.session_id);
          if (sid) useFlowRunStore.getState().restore(sid, payload.flow_run, Number(payload.flow_run_event_seq || asRecord(payload.flow_run).event_seq || 0));
        }
        if (source === "flow_event") {
          const sid = normalizeSessionId(payload.session_id || msg.session_id);
          if (sid) {
            useFlowRunStore.getState().applyEvent({ ...payload, session_id: sid } as unknown as FlowEvent, replaySeq, Boolean(eventData.replayed));
            const run = useFlowRunStore.getState().runs[sid]?.find(run => run.flow_run_id === payload.flow_run_id);
            if (run) { setSessionRunning(sid, !terminalFlow(run.status)); setSessionThinking(sid, run.status === "running"); }
          }
          return;
        }
        // Live legacy frames are still consumed once. Only replayed, tagged
        // process frames are unpacked for the flow's collapsed step details.
        if (eventData.replayed && payload.flow_run_id && ["agent_workflow", "thinking", "response", "status", "error", "interrupt_request", "tool"].includes(source)) {
          msg = { ...msg, type: source, content: String(payload.content || ""), phase: String(payload.phase || ""), agent: String(payload.agent || ""), agent_display_name: String(payload.agent_display_name || ""), data: payload };
        }
      }

      msg = normalizeIncomingMessage(msg);

      const normalizedAttachAckSessionId =
        resolveSessionAttachAckFromNormalizedMessage(msg);
      if (normalizedAttachAckSessionId) {
        promoteSessionAttachStateLive(normalizedAttachAckSessionId);
      }

      const attachBlocked = resolveSessionAttachBlockFromNormalizedMessage(msg);
      if (attachBlocked) {
        commitSessionAttachStates(
          markSessionAttachStateReplayRequired(
            useSessionStore.getState().sessionAttachState,
            attachBlocked.sessionId,
          ),
        );
        return;
      }

      if (msg.type === "skill_catalog_changed") {
        if (typeof window !== "undefined") {
          window.dispatchEvent(
            new CustomEvent("autocarto:skill_catalog_changed", {
              detail: msg.data || {},
            }),
          );
        }
        return;
      }

      if (msg.type === "runtime_snapshot") {
        const incomingSnapshot = msg.data as Partial<RuntimeSnapshot> | null;
        const currentSnapshot = useRuntimeSnapshotStore.getState().snapshot;
        if (shouldForceRefreshRuntimeSnapshot(currentSnapshot, incomingSnapshot)) {
          void fetchRuntimeSnapshot().catch(() => undefined);
          return;
        }
        applyRuntimeSnapshot(incomingSnapshot as never);
        return;
      }

      if (msg.type === "session_runtime_state") {
        const data = asRecord(msg.data);
        const runtimeSessionId = normalizeSessionId(
          data.session_id || msg.session_id,
        );
        if (!runtimeSessionId) {
          return;
        }
        setSessionRuntimeState(runtimeSessionId, {
          session_id: runtimeSessionId,
          run_id: String(data.run_id || "").trim() || undefined,
          status:
            String(data.status || "")
              .trim()
              .toLowerCase() || undefined,
          execution_mode: String(data.execution_mode || "").trim() || undefined,
          phase: String(data.phase || "").trim() || undefined,
          bound_instance_id:
            String(data.bound_instance_id || "").trim() || undefined,
          bound_project_path:
            String(data.bound_project_path || "").trim() || undefined,
          checkpoint_id: String(data.checkpoint_id || "").trim() || undefined,
          runtime_revision:
            String(data.runtime_revision || "").trim() || undefined,
          active_agent_config:
            String(data.active_agent_config || "").trim() || undefined,
          last_error: String(data.last_error || "").trim() || undefined,
          failure_code: String(data.failure_code || "").trim() || undefined,
          failure_reason: String(data.failure_reason || "").trim() || undefined,
          updated_at: String(data.updated_at || "").trim() || undefined,
        });
        const status = String(data.status || "")
          .trim()
          .toLowerCase();
        const running = status === "running" || status === "interrupted";
        setSessionRunning(runtimeSessionId, running);
        setSessionThinking(runtimeSessionId, status === "running");
        if (status === "completed") {
          completeSessionWorkflowBlock(runtimeSessionId, "completed");
        } else if (status === "cancelled") {
          clearSessionInterrupts(runtimeSessionId);
          completeSessionWorkflowBlock(runtimeSessionId, "completed");
        } else if (status === "failed") {
          completeSessionWorkflowBlock(runtimeSessionId, "error");
        }

        const activeRuntimeSessionId = useChatStore.getState().sessionId;
        if (runtimeSessionId === activeRuntimeSessionId) {
          const mode = resolveExecutionMode(
            data.execution_mode,
            data.run_intent,
          );
          if (mode) {
            setExecutionMode(mode);
          }
          if (
            typeof data.active_agent_config === "string" &&
            data.active_agent_config.trim()
          ) {
            setActiveAgentConfig(data.active_agent_config);
          }
        }
        return;
      }

      if (msg.type === "session_list_changed") {
        const data = asRecord(msg.data);
        const changeType = String(data.change_type || "")
          .trim()
          .toLowerCase();
        const changedSessionId = normalizeSessionId(data.session_id);

        if (changeType === "deleted" && changedSessionId) {
          removeSession(changedSessionId);
          purgeSession(changedSessionId);
          useFlowRunStore.getState().purge(changedSessionId);
          removeRunEventSeq(changedSessionId);
          getGlobalPersistenceController().markSessionDeleted(changedSessionId);
        } else if (
          (changeType === "created" || changeType === "updated") &&
          changedSessionId
        ) {
          void (async () => {
            try { if (!activeProjectPath.trim()) {
              await refreshSessions();
              return;
            }
            const loaded = await api.getSession(
              changedSessionId,
              activeProjectPath,
            )
            upsertSession(loaded) } catch (error) { void error; await refreshSessions() }
          })();
        } else {
          void refreshSessions();
        }

        if (typeof window !== "undefined") {
          window.dispatchEvent(
            new CustomEvent("autocarto:session_list_changed", {
              detail: data,
            }),
          );
        }
        return;
      }

      const activeSessionId = useChatStore.getState().sessionId;
      let sessionId = normalizeSessionId(msg.session_id);
      if (!sessionId && sessionScopedMessageTypes.has(msg.type)) {
        sessionId = normalizeSessionId(activeSessionId);
      }
      const activeMessage = isActiveSessionMessage(sessionId, activeSessionId);

      if (msg.type === "flow_event") {
        const event = asRecord(msg.data);
        const sid = normalizeSessionId(event.session_id || msg.session_id);
        if (!sid || !event.event || !event.payload) return;
        useFlowRunStore.getState().applyEvent({ ...event, session_id: sid } as unknown as FlowEvent);
        const run = useFlowRunStore.getState().runs[sid]?.find(run => run.flow_run_id === event.flow_run_id);
        if (String(event.event) === "flow.blocked") { setSessionRunning(sid, false); setSessionThinking(sid, false); }
        else if (run) { setSessionRunning(sid, !terminalFlow(run.status)); setSessionThinking(sid, run.status === "running"); }
        return;
      }

      const processData = asRecord(msg.data);
      if ((msg.phase === "tool_result" || msg.type === "tool") && processData.tool === "flow_preview") {
        const preview = parseFlowPreview(processData.result || msg.content);
        if (preview) {
          appendSessionAwareMessage(sessionId, activeMessage, { type: "status", content: `流程预览：${preview.name || preview.plan.name}`, data: { ...processData, flow_preview: preview } });
          return;
        }
      }
      if (processData.flow_run_id && ["agent_workflow", "thinking", "response", "status", "error", "tool", "agent"].includes(msg.type)) {
        const type = msg.phase === "tool_call" || msg.phase === "tool_result" || msg.type === "tool" ? "tool" : msg.type === "thinking" || msg.phase === "thinking" ? "thinking" : msg.type === "response" ? "assistant" : msg.type === "error" ? "error" : "status";
        appendSessionAwareMessage(sessionId, activeMessage, { type, content: msg.content || "", phase: msg.phase, agent: msg.agent, agentDisplayName: msg.agent_display_name, data: processData });
        return;
      }

      const unsupportedContractCode = resolveUnsupportedContractCode(msg);
      if (unsupportedContractCode) {
        const detail = resolveUnsupportedContractMessage(msg);
        useConnectionStore.setState({
          startupError: detail,
          backendReady: false,
        });
        hasEverLoadedRef.current = false;
        setConnected(false);
        setFailed(true);
        appendSessionAwareMessage(sessionId, activeMessage, {
          type: "error",
          content: detail,
          data: asRecord(msg.data),
        });
        return;
      }

      if (msg.type === "task_running_changed") {
        if (!sessionId) {
          return;
        }
        const data = asRecord(msg.data);
        const running = Boolean(data.running);
        setSessionRunning(sessionId, running);
        if (!running) {
          setSessionThinking(sessionId, false);
          completeSessionWorkflowBlock(sessionId, "completed");
        }
        return;
      }

      if (msg.type === "thinking") {
        if (!sessionId) {
          return;
        }
        setSessionRunning(sessionId, true);
        setSessionThinking(sessionId, true);
        appendWorkflowEvent(sessionId, {
          ...msg,
          type: "agent_workflow",
          phase: msg.phase || "thinking",
        });
        return;
      }

      if (msg.type === "response") {
        if (sessionId) {
          setSessionThinking(sessionId, false);
          setSessionRunning(sessionId, false);
          completeSessionWorkflowBlock(sessionId, "completed");
        }
        if (msg.content) {
          appendSessionAwareMessage(sessionId, activeMessage, {
            type: "assistant",
            content: msg.content,
            agent: msg.agent,
            agentDisplayName: msg.agent_display_name,
            phase: msg.phase,
            data: msg.data,
          });
        }
        return;
      }

      if (msg.type === "mode_state") {
        const data = asRecord(msg.data);
        const mode = resolveExecutionMode(data.mode, data.run_intent);
        const phase = parseAutoPhase(data.phase);

        if (activeMessage && mode) {
          setExecutionMode(mode);
        }

        if (activeMessage) {
          setAutoPhase("idle");
        }

        if (phase === "completed" && sessionId) {
          setSessionThinking(sessionId, false);
          setSessionRunning(sessionId, false);
        }
        return;
      }

      if (msg.type === "phase_transition") {
        const data = asRecord(msg.data);
        const planSummary = String(data.plan_summary || msg.content || "");
        if (sessionId) {
          completeSessionWorkflowBlock(sessionId, "completed");
          setSessionRunning(sessionId, true);
        }
        if (activeMessage) {
          setAutoPhase("idle");
        }
        appendSessionAwareMessage(sessionId, activeMessage, {
          type: "phase_transition",
          content: planSummary,
          data,
        });
        return;
      }

      if (msg.type === "error") {
        if (sessionId) {
          setSessionThinking(sessionId, false);
          setSessionRunning(sessionId, false);
          completeSessionWorkflowBlock(sessionId, "error");
        }
        appendSessionAwareMessage(sessionId, activeMessage, {
          type: "error",
          content: msg.content || "后端返回错误",
          data: msg.data,
        });
        return;
      }

      if (msg.type === "cancelled") {
        if (sessionId) {
          setSessionThinking(sessionId, false);
          setSessionRunning(sessionId, false);
          clearSessionInterrupts(sessionId);
          completeSessionWorkflowBlock(sessionId, "completed");
        }
        appendSessionAwareMessage(sessionId, activeMessage, {
          type: "status",
          content: msg.content || "任务已取消",
        });
        return;
      }

      if (msg.type === "task_list_update") {
        if (!sessionId) {
          return;
        }
        const todos = parseTodos(msg);
        setSessionTasks(sessionId, todos);
        const currentBlocks =
          useChatStore.getState().sessionWorkflowBlocks[sessionId] || [];
        const nextBlocks = reconcileWorkflowBlocksWithTaskListUpdate(
          currentBlocks,
          Date.now(),
        );
        if (nextBlocks !== currentBlocks) {
          setSessionWorkflowBlocks(sessionId, nextBlocks);
        }
        return;
      }

      if (msg.type === "interrupt_request") {
        if (!sessionId) {
          return;
        }
        setSessionRunning(sessionId, true);
        setSessionThinking(sessionId, false);
        const data = asRecord(msg.data) as InterruptRequestData;
        addSessionInterrupt(sessionId, data);
        if (parseReviewReference(data, sessionId)) {
          appendSessionAwareMessage(sessionId, activeMessage, {
            type: "interrupt",
            content: msg.content || "知识抽取结果已生成，可查看、修改或开始制图。",
            data: asRecord(msg.data),
          });
        }
        return;
      }

      if (msg.type === "context.response") {
        if (!activeMessage) {
          return;
        }
        const data = asRecord(msg.data) as ContextResponseData;
        setLastContextResponse(data);
        if (typeof data.estimated_tokens === "number") {
          setContextStats({
            messageCount: Number(data.message_count || 0),
            totalChars: Number(data.total_chars || 0),
            estimatedTokens: Number(data.estimated_tokens || 0),
          });
        } else if (typeof data.compressed_count === "number") {
          setContextStats({
            messageCount: Number(data.compressed_count || 0),
            totalChars: Number(data.total_chars || 0),
            estimatedTokens: Number(data.estimated_tokens || 0),
          });
        }

        if (data.success === false) {
          addMessage({
            type: "error",
            content: String(data.error || "上下文操作失败"),
            data: asRecord(msg.data),
          });
        } else if (msg.content) {
          addMessage({
            type: "context",
            content: msg.content,
            data: asRecord(msg.data),
          });
        }
        return;
      }

      if (msg.type === "decision") {
        if (sessionId) {
          appendWorkflowEvent(sessionId, {
            ...msg,
            type: "agent_workflow",
            phase: "decision",
          });
        }
        if (msg.content) {
          appendSessionAwareMessage(sessionId, activeMessage, {
            type: "decision",
            content: msg.content,
            data: asRecord(msg.data),
          });
        }
        return;
      }

      if (msg.type === "status") {
        const data = asRecord(msg.data);
        if (sessionId && String(data.status || "") === "thinking") {
          setSessionRunning(sessionId, true);
        }
        if (msg.content) {
          appendSessionAwareMessage(sessionId, activeMessage, {
            type: "status",
            content: msg.content,
            data,
          });
        }
        return;
      }

      if (msg.type === "session_state") {
        const data = asRecord(msg.data);
        const snapshotSessionId =
          sessionId || normalizeSessionId(data.session_id);
        const isSessionSnapshot = Boolean(snapshotSessionId);
        const mode = resolveExecutionMode(data.execution_mode, data.run_intent);
        if (mode && (!isSessionSnapshot || activeMessage)) {
          setExecutionMode(mode);
          setAutoPhase("idle");
        }

        if (
          typeof data.active_model === "string" &&
          (!isSessionSnapshot || activeMessage)
        ) {
          setActiveModelName(data.active_model);
        }
        if (
          typeof data.active_agent_config === "string" &&
          (!isSessionSnapshot || activeMessage)
        ) {
          setActiveAgentConfig(data.active_agent_config);
        }
        if (Array.isArray(data.available_models) && !isSessionSnapshot) {
          const models = data.available_models
            .filter((item) => item && typeof item === "object")
            .map((item) => {
              const payload = item as Record<string, unknown>;
              return {
                name: String(payload.name || ""),
                model: String(payload.model || ""),
                provider: String(payload.provider || ""),
                has_api_key: Boolean(payload.has_api_key),
                supports_thinking: Boolean(payload.supports_thinking),
                supports_plan_execute: Boolean(payload.supports_plan_execute),
                supports_forced_tool_choice: Boolean(
                  payload.supports_forced_tool_choice,
                ),
                ready_for_chat: Boolean(payload.ready_for_chat),
                ready_for_plan: Boolean(payload.ready_for_plan),
                ready_for_auto_title: Boolean(payload.ready_for_auto_title),
                readiness_reason: String(payload.readiness_reason || ""),
              };
            })
            .filter((item) => item.name);
          setAvailableModels(models);
        }
        if (Array.isArray(data.available_agent_configs) && !isSessionSnapshot) {
          const names = data.available_agent_configs
            .filter((item) => item && typeof item === "object")
            .map((item) => {
              const payload = item as Record<string, unknown>;
              return String(payload.config_name || "");
            })
            .filter(Boolean);
          setAvailableAgentConfigs(names);
        }
        if (snapshotSessionId) {
          if (Object.prototype.hasOwnProperty.call(data, "flow_run")) useFlowRunStore.getState().restore(snapshotSessionId, data.flow_run, Number(data.flow_run_event_seq || asRecord(data.flow_run).event_seq || 0));
          const restoredTasks = toTodoItems(data.tasks ?? data.todos ?? data.items);
          if (restoredTasks.length > 0) {
            setSessionTasks(snapshotSessionId, restoredTasks);
          }

          const restoredWorkflowBlocks = toWorkflowBlocks(data.workflow_blocks);
          if (restoredWorkflowBlocks.length > 0) {
            setSessionWorkflowBlocks(snapshotSessionId, restoredWorkflowBlocks);
            const runningBlock = restoredWorkflowBlocks.find(
              (item) => item.status === "running",
            );
            setSessionActiveWorkflowBlockId(
              snapshotSessionId,
              runningBlock?.id || null,
            );
            setSessionFocusedWorkflowBlockId(
              snapshotSessionId,
              runningBlock?.id || null,
            );
          }

          const snapshotStatus = String(
            data.status || data.runtime_status || "",
          )
            .trim()
            .toLowerCase();
          if (snapshotStatus) {
            setSessionRuntimeState(snapshotSessionId, {
              session_id: snapshotSessionId,
              run_id: String(data.run_id || "").trim() || undefined,
              status: snapshotStatus || undefined,
              execution_mode:
                String(data.execution_mode || "").trim() || undefined,
              phase:
                String(data.phase || data.runtime_phase || "").trim() ||
                undefined,
              bound_instance_id:
                String(data.bound_instance_id || "").trim() || undefined,
              bound_project_path:
                String(data.bound_project_path || "").trim() || undefined,
              checkpoint_id:
                String(data.checkpoint_id || "").trim() || undefined,
              runtime_revision:
                String(data.runtime_revision || "").trim() || undefined,
              active_agent_config:
                String(data.active_agent_config || "").trim() || undefined,
              last_error: String(data.last_error || "").trim() || undefined,
              failure_code:
                String(
                  data.failure_code || data.runtime_failure_code || "",
                ).trim() || undefined,
              failure_reason:
                String(
                  data.failure_reason || data.runtime_failure_reason || "",
                ).trim() || undefined,
              updated_at: String(data.updated_at || "").trim() || undefined,
            });
            const running =
              snapshotStatus === "running" || snapshotStatus === "interrupted";
            setSessionRunning(snapshotSessionId, running);
            if (!running) {
              setSessionThinking(snapshotSessionId, false);
            }
            if (snapshotStatus === "cancelled") {
              clearSessionInterrupts(snapshotSessionId);
              completeSessionWorkflowBlock(snapshotSessionId, "completed");
            }
          }
        }
        return;
      }

      if (msg.type === "agent_workflow") {
        if (!sessionId) {
          return;
        }
        setSessionRunning(sessionId, true);
        if (String(msg.phase || "").toLowerCase() === "thinking") {
          setSessionThinking(sessionId, true);
        } else if (String(msg.phase || "").toLowerCase() === "progress") {
          setSessionThinking(sessionId, false);
        }
        appendWorkflowEvent(sessionId, msg);
      }
    },
    [
      activeProjectPath,
      addMessage,
      addSessionInterrupt,
      applyRuntimeSnapshot,
      appendSessionAwareMessage,
      appendWorkflowEvent,
      clearSessionInterrupts,
      commitSessionAttachStates,
      completeSessionWorkflowBlock,
      fetchRuntimeSnapshot,
      purgeSession,
      refreshSessions,
      removeRunEventSeq,
      removeSession,
      setActiveAgentConfig,
      setActiveModelName,
      setAvailableAgentConfigs,
      setAvailableModels,
      setContextStats,
      setExecutionMode,
      setAutoPhase,
      setLastContextResponse,
      setConnected,
      setSessionRunning,
      setSessionActiveWorkflowBlockId,
      setSessionRuntimeState,
      setSessionWorkflowBlocks,
      setSessionTasks,
      setSessionThinking,
      setSessionFocusedWorkflowBlockId,
      upsertRunEventSeq,
      upsertSession,
      promoteSessionAttachStateLive,
    ],
  );

  useEffect(() => {
    realtimeMessageHandlerRef.current = handleRealtimeMessage;
  }, [handleRealtimeMessage]);

  useEffect(() => {
    refreshSessionsRef.current = refreshSessions;
  }, [refreshSessions]);

  useEffect(() => {
    syncSessionAttachmentsRef.current = syncSessionAttachments;
    syncSessionAttachments();
  }, [syncSessionAttachments]);

  useEffect(() => {
    let cancelled = false;
    const disposeConnection = wsClient.onConnection((connected) => {
      const wasConnected = wasConnectedRef.current;
      wasConnectedRef.current = connected;
      setConnected(connected);
      if (!connected) {
        attachedSessionsRef.current = new Set();
        if (wasConnected) {
          reattachPendingRef.current = true;
          syncSessionAttachmentsRef.current();
        }
        return;
      }
      if (reattachPendingRef.current) {
        reattachPendingRef.current = false;
      }
      if (!wasConnected) {
        void fetchRuntimeSnapshot().catch(() => undefined);
        void refreshSessionsRef.current();
      }
      syncSessionAttachmentsRef.current();
    });
    const disposeMessages = wsClient.onMessage((msg) => {
      realtimeMessageHandlerRef.current(msg);
    });

    setChecking(true);
    setFailed(false);
    void (async () => {
      await initializeRuntime();
      const runtimeState = useConnectionStore.getState();
      const allowShellWithoutRuntime =
        !isDesktopRuntime() &&
        !runtimeState.backendUrl.trim() &&
        !runtimeState.wsUrl.trim();

      if (allowShellWithoutRuntime) {
        if (cancelled) {
          return;
        }
        setConnected(false);
        setChecking(false);
        setFailed(false);
        return;
      }

      api.setBaseURL(runtimeState.backendUrl);
      wsClient.setUrl(runtimeState.wsUrl);

      const ok = await waitForBackend();
      if (cancelled) {
        return;
      }
      if (!ok) {
        const latestRuntimeState = useConnectionStore.getState();
        const shouldDeferFatalShell =
          isDesktopRuntime() &&
          Boolean(latestRuntimeState.backendUrl.trim()) &&
          Boolean(latestRuntimeState.wsUrl.trim()) &&
          !isStickyUnsupportedContract(latestRuntimeState.startupError);
        setConnected(false);
        setChecking(false);
        setFailed(!shouldDeferFatalShell);
        if (shouldDeferFatalShell) {
          hasEverLoadedRef.current = true;
        }
        return;
      }

      wsClient.connect();
      void fetchRuntimeSnapshot().catch(() => undefined);
      const startupErrorAfterConnect = useConnectionStore.getState().startupError;
      if (!isStickyUnsupportedContract(startupErrorAfterConnect)) {
        hasEverLoadedRef.current = true;
      }
      setChecking(false);
    })().catch((error: unknown) => {
      if (cancelled) {
        return;
      }
      const existingError = useConnectionStore.getState().startupError;
      const detail = isStickyUnsupportedContract(existingError)
        ? existingError
        : typeof error === "string" && error.trim()
          ? error.trim()
          : getApiErrorMessage(error, "读取后端运行信息失败，请重试。");
      useConnectionStore.setState({ startupError: detail, backendReady: false });
      if (isStickyUnsupportedContract(detail)) {
        hasEverLoadedRef.current = false;
      }
      setConnected(false);
      setFailed(true);
    }).finally(() => {
      if (!cancelled) {
        setChecking(false);
      }
    });

    return () => {
      cancelled = true;
      wasConnectedRef.current = false;
      reattachPendingRef.current = false;
      disposeConnection();
      disposeMessages();
      wsClient.disconnect();
      setConnected(false);
    };
  }, [
    fetchRuntimeSnapshot,
    initializeRuntime,
    retryToken,
    setConnected,
    waitForBackend,
  ]);

  useEffect(() => {
    let cancelled = false;
    const timer = setInterval(() => {
      checkHealth().then((healthy) => {
        if (cancelled || !healthy) {
          return;
        }
  if (failed && process.env.NEXT_PUBLIC_SKIP_HEALTH_CHECK !== '1') {
          setFailed(false);
        }
        if (!wsClient.connected) {
          wsClient.connect();
          void fetchRuntimeSnapshot().catch(() => undefined);
          void refreshSessions();
        }
      });
    }, HEALTH_CHECK_INTERVAL_MS);

    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [checkHealth, failed, fetchRuntimeSnapshot, refreshSessions]);

  if (checking && !hasEverLoadedRef.current) {
    return (
      <div className="flex h-screen w-screen items-center justify-center bg-zinc-50 dark:bg-zinc-950">
        <div className="text-center space-y-3">
          <div className="animate-spin h-7 w-7 border-2 border-blue-500 border-t-transparent rounded-full mx-auto" />
          <p className="text-xs text-zinc-500 dark:text-zinc-400">正在连接后端服务...</p>
        </div>
      </div>
    );
  }

  if (failed && !hasEverLoadedRef.current) {
    return (
      <div className="flex h-screen w-screen items-center justify-center bg-zinc-50 dark:bg-zinc-950">
        <div className="max-w-xl px-6 text-center space-y-3">
          <p className="text-red-600 dark:text-red-400 font-medium">无法连接到后端服务</p>
          <p className="text-xs text-zinc-500 dark:text-zinc-400">
            {startupError || "请检查 carto-agent 是否正常运行"}
          </p>
          <button
            className="px-3 py-2 text-xs bg-blue-600 text-white rounded-sm hover:bg-blue-700"
            onClick={() => {
              setRetryToken((prev) => prev + 1);
            }}
          >
            重试
          </button>
        </div>
      </div>
    );
  }

  return <>{children}</>;
}
