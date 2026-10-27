'use client'

import { useMemo } from 'react'
import ChatCapabilityBanner from './ChatCapabilityBanner'
import ChatInput from './ChatInput'
import ContextToolbar from './ContextToolbar'
import InterruptCard from './InterruptCard'
import MessageList from './MessageList'
import SessionWorkspace from '@/components/workspace/SessionWorkspace'
import { useSessionStore } from '@/stores/sessionStore'
import ThinkingIndicator from './ThinkingIndicator'
import ConfirmDialog from '@/components/common/ConfirmDialog'
import { cn } from '@/lib/utils'
import { useChatContext } from '@/hooks/useChatContext'
import { useChatMessaging } from '@/hooks/useChatMessaging'
import { useChatRuntimeConfig } from '@/hooks/useChatRuntimeConfig'
import { useChatSession } from '@/hooks/useChatSession'
import { useInstanceStore } from '@/stores/instanceStore'
import { useKnowledgeReview } from '@/features/knowledge-review/useKnowledgeReview'
import { parseReviewReference } from '@/features/knowledge-review/model'
import KnowledgeReviewDialog from '@/features/knowledge-review/KnowledgeReviewDialog'
import { useFlowComposer } from '@/features/flow-run/useFlowComposer'
import FlowComposer from '@/features/flow-run/FlowComposer'
import { useFlowRunStore } from '@/features/flow-run/store'
import { useChatStore } from '@/stores/chatStore'

export default function ChatPanel() {
  const review = useKnowledgeReview()
  const session = useChatSession()
  const runtimeConfig = useChatRuntimeConfig({
		loadSessionSettings: session.loadSessionSettings,
  })
  const context = useChatContext({
    ensureSession: session.ensureSession,
    createSnapshotForSession: session.createSnapshotForSession,
    sessionSettings: session.sessionSettings,
    applySessionRecord: session.applySessionRecord,
    sessionRef: session.sessionRef,
    planUnavailableReason: runtimeConfig.planUnavailableReason,
  })
  const messaging = useChatMessaging({
    ensureSession: session.ensureSession,
  })
  const composer = useFlowComposer(messaging.handleSend)
  const flowSessionId = useChatStore(state => state.sessionId) || ''
  const workspaceProjectPath = useSessionStore(state => state.sessions.find(record => record.session_id === flowSessionId)?.project_path || '')
  const flowRuns = useFlowRunStore(state => state.runs)
  const blockedFlows = useFlowRunStore(state => state.blocked)
  const instances = useInstanceStore((s) => s.instances)
  const activeInstanceId = useInstanceStore((s) => s.activeInstanceId)
  const runningCount = useInstanceStore((s) => s.runningCount)
  const stale = useInstanceStore((s) => s.stale)

  const activeInstance = useMemo(
    () => instances.find((item) => item.id === activeInstanceId) || null,
    [activeInstanceId, instances],
  )
  const hasProjectPath = String(activeInstance?.project_path || '').trim().length > 0

  const chatDisabled = !messaging.connected || !messaging.projectConnected

  return (
    <div
      className={cn(
        'relative flex h-full min-h-0 overflow-hidden transition-colors duration-200',
        context.executionMode === 'build'
          ? 'bg-accent/5 dark:bg-accent/10'
          : 'bg-chip-amber-bg dark:bg-chip-amber-bg',
      )}
    >
      <div data-testid="chat-content" className="flex min-w-0 flex-1 flex-col overflow-hidden">
        <ChatCapabilityBanner
          activeInstanceId={activeInstanceId}
          connected={messaging.connected}
          hasProjectPath={hasProjectPath}
          projectConnected={messaging.projectConnected}
          runningCount={runningCount}
          stale={stale}
        />

        <div className="relative min-h-0 flex-1 overflow-hidden">
          <MessageList
            messages={messaging.messages}
            flowRuns={flowRuns[flowSessionId]}
            flowBlocked={blockedFlows[flowSessionId]}
            onResumeFlow={messaging.handleResumeFlow}
            workflowBlocks={messaging.workflowBlocks}
            focusedWorkflowBlockId={session.focusedWorkflowBlockId}
            onOpenWorkflowBlock={session.handleOpenWorkflowBlock}
            pendingInterrupts={messaging.pendingInterrupts}
            interruptSubmitting={messaging.interruptSubmitting}
            interruptResolvedStates={messaging.resolvedInterruptStates}
            onInterruptApprove={messaging.handleInterruptApprove}
            onInterruptReject={messaging.handleInterruptReject}
            executionMode={context.executionMode}
            sessionId={review.sessionId}
            knowledgeReviewSummaries={review.summaries}
            onOpenKnowledgeReview={(reference) => { void review.show(reference) }}
            onConfirmKnowledgeReview={(reference) => { void review.show(reference, true) }}
          />
        </div>

        {messaging.isThinking && <ThinkingIndicator />}

        {messaging.activeInterrupt && !messaging.activeInterrupt.flow_run_id && !parseReviewReference(messaging.activeInterrupt) && (
          <div className="mx-auto w-full max-w-4xl shrink-0 px-3 pb-1.5">
            <InterruptCard
              interrupt={messaging.activeInterrupt}
              submitting={messaging.interruptSubmitting === messaging.activeInterruptCheckpointId}
              resolvedState={messaging.resolvedInterruptStates[messaging.activeInterruptCheckpointId]}
              onApprove={(payload) => {
                messaging.handleInterruptApprove(messaging.activeInterruptCheckpointId, payload)
              }}
              onReject={() => {
                messaging.handleInterruptReject(messaging.activeInterruptCheckpointId)
              }}
            />
          </div>
        )}

        <ChatInput
          onSend={composer.send}
          sendBlocked={composer.blocked}
          flowControls={<FlowComposer composer={composer} disabled={chatDisabled || messaging.isRunning} mode={context.executionMode}/>}
          onCancel={messaging.handleCancel}
          disabled={chatDisabled}
          running={messaging.isRunning}
          executionMode={context.executionMode}
        />

        <ContextToolbar
          executionMode={context.executionMode}
          thinkingEnabled={context.thinkingEnabled}
          activeModelName={runtimeConfig.activeModelName}
          activeAgentConfig={runtimeConfig.activeAgentConfig}
          models={runtimeConfig.availableModels}
          agentConfigs={runtimeConfig.availableAgentConfigs}
          messageCount={context.contextStats.messageCount}
          estimatedTokens={context.contextStats.estimatedTokens}
          totalChars={context.contextStats.totalChars}
          planEnabled={runtimeConfig.planEnabled}
          planIssue={runtimeConfig.planIssueMessage}
          onExecutionModeChange={context.handleExecutionModeChange}
          onThinkingToggle={context.handleThinkingToggle}
          onModelSwitch={runtimeConfig.handleModelSwitch}
          onAgentConfigSwitch={runtimeConfig.handleAgentConfigSwitch}
          onCompress={context.handleCompressContext}
          onCreateSnapshot={session.handleCreateSnapshotQuick}
          onRestoreSnapshot={session.handleRestoreSnapshotQuick}
          recentSnapshots={session.recentSnapshots}
          modelLoadError={runtimeConfig.modelLoadError}
          agentConfigLoadError={runtimeConfig.agentConfigLoadError}
          onReloadRuntimeConfig={runtimeConfig.loadRuntimeConfig}
          disabled={chatDisabled}
          running={messaging.isRunning}
        />
      </div>

      <SessionWorkspace
          sessionId={flowSessionId}
          projectPath={workspaceProjectPath}
          tasks={messaging.tasks}
          pendingInterrupts={messaging.pendingInterrupts}
          workflowBlocks={messaging.workflowBlocks}
          focusedWorkflowBlockId={session.focusedWorkflowBlockId}
          onFocusWorkflowBlock={session.setFocusedWorkflowBlockId}
        />

      <KnowledgeReviewDialog controller={review} />
      <ConfirmDialog
        open={session.confirmDialog.open}
        title={session.confirmDialog.title}
        description={session.confirmDialog.description}
        confirmText={session.confirmDialog.confirmText}
        danger={session.confirmDialog.danger}
        loading={session.confirmingAction}
        onConfirm={() => {
          void session.runConfirmDialogAction()
        }}
        onCancel={session.closeConfirmDialog}
      />
    </div>
  )
}
