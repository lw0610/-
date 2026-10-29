'use client'
import { useEffect, useMemo, useRef, useState } from 'react'
import type {
  InterruptRequestData,
  Message,
  TodoItem,
  WorkflowBlock,
} from '@/types/message'
import { useUIStore, type WorkspaceTab } from '@/stores/uiStore'
import { useSessionFiles, useSessionFilesStore } from '@/hooks/useSessionFiles'
import wsClient from '@/lib/websocket'
import { localFileAction, type SessionFile } from './files'
import FilesTab from './FilesTab'
import FilePreview from './FilePreview'
import TasksTab from './TasksTab'
import ProcessTab from './ProcessTab'
import { useFlowRunStore } from '@/features/plan/store'
import { executionTasks, executionWorkflowBlocks, workspaceRuns } from '@/features/plan/workspace'
const emptyMessages: Message[] = []
interface Props {
  sessionId: string
  projectPath: string
  tasks: TodoItem[]
  pendingInterrupts: InterruptRequestData[]
  workflowBlocks: WorkflowBlock[]
  messages?: Message[]
  focusedWorkflowBlockId: string | null
  onFocusWorkflowBlock: (id: string | null) => void
}
export function workspaceUsesDrawer(
  mainWidth: number,
  panelWidth: number,
  windowWidth: number,
) {
  return windowWidth <= 900 || mainWidth - panelWidth - 40 < 420
}
function Icon({ tab }: { tab: WorkspaceTab | 'folder' }) {
  return (
    <svg
      width="18"
      height="18"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.7"
      aria-hidden="true"
    >
      {tab === 'files' ? (
        <>
          <path d="M6 3h8l4 4v14H6z" />
          <path d="M14 3v5h4M9 12h6M9 16h6" />
        </>
      ) : tab === 'tasks' ? (
        <>
          <path d="M9 6h12M9 12h12M9 18h12M3 6l1 1 2-3M3 12l1 1 2-3M3 18l1 1 2-3" />
        </>
      ) : tab === 'process' ? (
        <>
          <rect x="3" y="3" width="6" height="6" rx="1" />
          <rect x="15" y="15" width="6" height="6" rx="1" />
          <path d="M6 9v9h9M9 6h9v9" />
        </>
      ) : (
        <path d="M3 6h7l2 2h9v12H3z" />
      )}
    </svg>
  )
}
export default function SessionWorkspace(props: Props) {
  const {
    sessionId,
    projectPath,
    tasks,
    pendingInterrupts,
    workflowBlocks,
    messages = emptyMessages,
    focusedWorkflowBlockId,
    onFocusWorkflowBlock,
  } = props
  const flowRuns = useFlowRunStore(state => state.runs[sessionId])
  const plans = useFlowRunStore(state => state.plans[sessionId])
  const runs = useMemo(() => workspaceRuns(flowRuns || [], plans || []), [flowRuns, plans])
  const visibleTasks = useMemo(() => executionTasks(runs, plans || [], tasks), [runs, plans, tasks])
  const visibleBlocks = useMemo(() => executionWorkflowBlocks(runs, messages, workflowBlocks), [runs, messages, workflowBlocks])
  const workspace = useUIStore((state) => state.workspace),
    collapsed = useUIStore((state) => state.sessionPanelCollapsed)
  // Both saved tab names and existing "view details" links open the combined
  // inspector. Keep their targets compatible without migrating user storage.
  const activeTab = workspace.tab === 'files' ? 'files' : 'tasks'
  const setWorkspace = useUIStore((state) => state.setWorkspace),
    markViewed = useUIStore((state) => state.markFilesViewed)
  const [preview, setPreview] = useState<SessionFile | null>(null),
    [fullscreen, setFullscreen] = useState(false),
    [mainWidth, setMainWidth] = useState(1000),
    [windowWidth, setWindowWidth] = useState(
      typeof window === 'undefined' ? 1200 : window.innerWidth,
    ),
    [feedback, setFeedback] = useState('')
  const root = useRef<HTMLDivElement>(null),
    files = useSessionFiles(sessionId, projectPath)
  const refreshFiles = files.refresh
  const panelWidth = preview
    ? Math.min(workspace.previewWidth, Math.max(420, mainWidth * 0.6))
    : workspace.width
  const drawer = workspaceUsesDrawer(mainWidth, panelWidth, windowWidth)
  useEffect(() => {
    const parent = root.current?.parentElement
    if (!parent) return
    const update = () => {
      setMainWidth(
        parent.getBoundingClientRect().width || window.innerWidth - 152,
      )
      setWindowWidth(window.innerWidth)
    }
    update()
    const observer =
      typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(update)
    observer?.observe(parent)
    window.addEventListener('resize', update)
    return () => {
      observer?.disconnect()
      window.removeEventListener('resize', update)
    }
  }, [])
  useEffect(() => {
    if (workspace.open && mainWidth - panelWidth - 40 < 420 && !collapsed)
      useUIStore.getState().setSessionPanelCollapsed(true)
  }, [workspace.open, mainWidth, panelWidth, collapsed])
  useEffect(() => {
    setPreview(null)
    setFullscreen(false)
  }, [sessionId])
  useEffect(() => {
    if (workspace.open && workspace.tab === 'files' && sessionId)
      markViewed(sessionId)
  }, [workspace.open, workspace.tab, sessionId, files.items, markViewed])
  useEffect(() => {
    let live = true
    const unsubscribe = wsClient.onMessage((message) => {
      const event = message.data
      if (
        message.type === 'flow_event' &&
        message.session_id === sessionId &&
        event?.event === 'flow.finished'
      ) {
        const payload = event.payload as Record<string, unknown> | undefined
        const deliverables = payload?.deliverables
        void refreshFiles().then(() => {
          const ui = useUIStore.getState()
          if (
            live &&
            ui.workspace.open &&
            deliverables &&
            typeof deliverables === 'object' &&
            Object.keys(deliverables).length
          ) {
            setPreview(null)
            const paths = JSON.stringify(deliverables)
            const files = useSessionFilesStore.getState().items[sessionId] || []
            ui.openWorkspace(
              'files',
              files
                .filter(
                  (item) =>
                    paths.includes(item.path) ||
                    (item.flow_run_id === event.flow_run_id &&
                      item.group === 'deliverable'),
                )
                .map((item) => item.id),
            )
          }
        })
      }
    })
    return () => {
      live = false
      unsubscribe()
    }
  }, [sessionId, refreshFiles])
  useEffect(() => {
    if (!fullscreen) return
    const escape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setFullscreen(false)
    }
    window.addEventListener('keydown', escape)
    return () => window.removeEventListener('keydown', escape)
  }, [fullscreen])
  const toggle = (tab: WorkspaceTab) => {
    if (workspace.open && activeTab === tab) setWorkspace({ open: false })
    else setWorkspace({ open: true, tab })
    setPreview(null)
  }
  const resize = (event: React.PointerEvent) => {
    event.preventDefault()
    const start = event.clientX,
      width = panelWidth
    const move = (next: PointerEvent) => {
      const value = width + start - next.clientX
      setWorkspace(
        preview
          ? {
              previewWidth: Math.max(
                420,
                Math.min(Math.max(420, mainWidth * 0.6), value),
              ),
            }
          : { width: Math.max(300, Math.min(560, value)) },
      )
    }
    const stop = () => {
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', stop)
    }
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', stop)
  }
  const taskCount =
    visibleTasks.filter(
      (task) =>
        !['completed', 'done', 'cancelled', 'skipped'].includes(String(task.status)),
    ).length + pendingInterrupts.length
  const activeProcess = visibleBlocks.some(
    (block) => block.status === 'running',
  )
  return (
    <div
      ref={root}
      className="flex h-full shrink-0"
      data-testid="session-workspace"
    >
      {workspace.open && drawer && (
        <button
          aria-label="收起工作区遮罩"
          className="absolute inset-0 z-30 bg-black/20"
          onClick={() => setWorkspace({ open: false })}
        />
      )}
      {workspace.open && (
        <aside
          aria-label="会话工作区"
          data-mode={drawer ? 'drawer' : 'panel'}
          className={`${drawer ? 'absolute inset-y-0 right-10 z-40 shadow-xl' : 'relative'} flex min-h-0 shrink-0 flex-col overflow-hidden border-l border-zinc-200 bg-[#fafafa] dark:border-zinc-800 dark:bg-zinc-950`}
          style={{
            width: drawer
              ? Math.min(panelWidth, Math.max(300, mainWidth - 64))
              : panelWidth,
          }}
        >
          {!drawer && (
            <div
              role="separator"
              aria-label="调整工作区宽度"
              aria-orientation="vertical"
              onPointerDown={resize}
              className="absolute inset-y-0 left-0 z-10 w-1 cursor-col-resize hover:bg-blue-400"
            />
          )}
          <div
            className="flex shrink-0 border-b border-zinc-200 px-2 pt-2 dark:border-zinc-800"
            role="tablist"
            aria-label="工作区页签"
          >
            {(['files', 'tasks'] as const).map((tab) => (
              <button
                key={tab}
                role="tab"
                aria-selected={activeTab === tab}
                onClick={() => {
                  setWorkspace({ tab })
                  setPreview(null)
                }}
                className={`flex-1 rounded-t px-2 py-2 text-xs ${activeTab === tab ? 'border-b-2 border-blue-500 bg-blue-50 text-blue-700 dark:bg-blue-950' : 'text-zinc-500 hover:bg-zinc-100 dark:hover:bg-zinc-900'}`}
              >
                {{ files: '文件', tasks: '任务与过程' }[tab]}
              </button>
            ))}
            <button
              aria-label="收起工作区"
              className="px-2 text-zinc-500"
              onClick={() => setWorkspace({ open: false })}
            >
              ×
            </button>
          </div>
          <div className="min-h-0 flex-1" hidden={workspace.tab !== 'files'}>
            {preview ? (
              <FilePreview
                key={preview.id}
                item={preview}
                sessionId={sessionId}
                projectPath={projectPath}
                onBack={() => setPreview(null)}
                onFullscreen={() => setFullscreen(true)}
              />
            ) : (
              <FilesTab
                files={files}
                sessionId={sessionId}
                projectPath={projectPath}
                onPreview={setPreview}
              />
            )}
          </div>
          <div className="min-h-0 flex-1" hidden={activeTab !== 'tasks'}>
            <div className="flex h-full min-h-0 flex-col" data-testid="workspace-task-process-panel">
              <div className="max-h-[40%] min-h-0 shrink-0 overflow-y-auto border-b border-zinc-200 scrollbar-thin dark:border-zinc-800" data-testid="workspace-task-scroll">
                <TasksTab tasks={visibleTasks} pendingInterrupts={pendingInterrupts} run={runs.at(-1)} embedded />
              </div>
              <div className="min-h-0 flex-1">
                <ProcessTab
                  workflowBlocks={visibleBlocks}
                  focusedWorkflowBlockId={focusedWorkflowBlockId}
                  onFocusWorkflowBlock={onFocusWorkflowBlock}
                />
              </div>
            </div>
          </div>
        </aside>
      )}
      <nav
        aria-label="会话工作区工具栏"
        className="relative z-40 flex w-10 shrink-0 flex-col items-center gap-2 border-l border-zinc-200 bg-zinc-50 py-3 dark:border-zinc-800 dark:bg-zinc-950"
      >
        {(['files', 'tasks'] as const).map((tab) => (
          <button
            key={tab}
            aria-label={`工作区${{ files: '文件', tasks: '任务与过程' }[tab]}`}
            title={
              {
                files: '本会话文件',
                tasks: '任务、待确认与智能体完整过程',
              }[tab]
            }
            aria-pressed={workspace.open && activeTab === tab}
            onClick={() => toggle(tab)}
            className={`relative rounded p-2 ${workspace.open && activeTab === tab ? 'bg-blue-100 text-blue-600 dark:bg-blue-950' : 'text-zinc-500 hover:bg-zinc-200 dark:hover:bg-zinc-800'}`}
          >
            <Icon tab={tab} />
            {tab === 'files' && files.newCount > 0 && (
              <span className="absolute -right-1 -top-1 rounded-full bg-blue-600 px-1 text-[9px] text-white">
                {files.newCount > 99 ? '99+' : files.newCount}
              </span>
            )}
            {tab === 'tasks' && taskCount > 0 && (
              <span
                className={`absolute -right-1 -top-1 rounded-full px-1 text-[9px] text-white ${pendingInterrupts.length ? 'bg-amber-500' : 'bg-blue-600'}`}
              >
                {taskCount}
              </span>
            )}
            {tab === 'tasks' && taskCount === 0 && activeProcess && (
              <span className="absolute right-0 top-0 h-2 w-2 rounded-full bg-blue-500" />
            )}
          </button>
        ))}
        <button
          aria-label="打开会话文件夹"
          title={feedback || '打开会话文件夹'}
          disabled={!files.sessionDir}
          className="mt-auto rounded p-2 text-zinc-500 disabled:opacity-30"
          onClick={() =>
            void localFileAction(files.sessionDir, 'open')
              .then(setFeedback)
              .catch((error) => setFeedback(String(error)))
          }
        >
          <Icon tab="folder" />
        </button>
      </nav>
      {fullscreen && preview && (
        <div
          role="dialog"
          aria-modal="true"
          aria-label="文件全屏预览"
          className="fixed inset-0 z-[100] bg-white p-4 dark:bg-zinc-950"
        >
          <button
            className="absolute right-5 top-4 z-10 rounded border bg-white px-3 py-1 text-xs dark:bg-zinc-900"
            onClick={() => setFullscreen(false)}
          >
            退出全屏
          </button>
          <FilePreview
            item={preview}
            sessionId={sessionId}
            projectPath={projectPath}
            onBack={() => setFullscreen(false)}
          />
        </div>
      )}
    </div>
  )
}
