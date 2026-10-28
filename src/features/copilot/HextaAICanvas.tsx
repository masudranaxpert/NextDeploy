import * as React from "react"
import {
  ChevronRightIcon,
  FileTextIcon,
  FolderIcon,
  FolderPlusIcon,
  ListChecksIcon,
  PanelLeftIcon,
  PlusIcon,
  RefreshCwIcon,
  SearchIcon,
  SquareIcon,
  UploadIcon,
  XIcon,
  Settings2,
  Trash2,
} from "lucide-react"

import { cn } from "@/lib/utils"
import { AskCard } from "@/components/ui/ask-card"
import { Button } from "@/components/ui/button"
import { Composer, ComposerFooter, ComposerFooterEnd, ComposerPick } from "@/components/ui/composer"
import { ContextRing } from "@/components/ui/context-ring"
import { Kbd } from "@/components/ui/kbd"
import { Panel, PanelRow } from "@/components/ui/panel"
import { Tag } from "@/components/ui/tag"
import { StepGroup, type Step } from "@/components/ui/tool-steps"
import { TurnStatus } from "@/components/ui/turn-status"
import { MarkdownContent } from "./MarkdownContent"
import { ProviderAuthModal } from "./ProviderAuthModal"
import { useCopilot } from "./useCopilot"
import type { ToolExecution, CopilotMessage } from "./types"
import { appsAndDockerApi } from "@/lib/api/appsApi"

// Project file node
type Node = { name: string; size?: string; children?: Node[] }

// Background task
type Task = {
  title: string
  kind: string
  state: string
  time: string
  tokens?: string
  activity?: string
  running?: boolean
}

const DEFAULT_TREE: Node[] = [
  {
    name: "src",
    children: [
      {
        name: "features",
        children: [
          {
            name: "copilot",
            children: [
              { name: "HextaAICanvas.tsx", size: "14 KB" },
              { name: "useCopilot.ts", size: "18 KB" },
              { name: "types.ts", size: "2 KB" },
            ],
          },
          { name: "workspace", children: [{ name: "AppWorkspace.tsx", size: "24 KB" }] },
        ],
      },
      { name: "components", children: [{ name: "ui", size: "64 KB" }] },
      { name: "index.css", size: "4 KB" },
      { name: "main.tsx", size: "2 KB" },
    ],
  },
  {
    name: "research",
    children: [
      { name: "ROADMAP.md", size: "2 KB" },
      { name: "baseline.yaml", size: "2 KB" },
    ],
  },
  { name: "docker-compose.yml", size: "4 KB" },
  { name: "package.json", size: "2 KB" },
  { name: "README.md", size: "5 KB" },
]

const MODES = [
  { value: "safe", label: "Ask first", hint: "Stops where the choice is yours" },
  { value: "autonomous", label: "On its own", hint: "Decides and carries on" },
]

const EFFORTS = [
  { value: "off", label: "Off", hint: "No reasoning steps" },
  { value: "low", label: "Low", hint: "Quick, concise thought" },
  { value: "medium", label: "Medium", hint: "Balanced exploration" },
  { value: "high", label: "High", hint: "Deeper problem solving" },
  { value: "max", label: "Max", hint: "Exhaustive reasoning chain" },
]

const INDENT = 12

/** Whether window matches media query */
const wide = (query: string) => (typeof window !== "undefined" ? window.matchMedia(query).matches : false)

/** Check if node or its descendants match search query */
const matches = (node: Node, query: string): boolean =>
  node.name.toLowerCase().includes(query) || !!node.children?.some((child) => matches(child, query))

/** Format raw bytes into human readable size */
function formatSize(bytes?: number): string {
  if (!bytes || bytes <= 0) return ""
  if (bytes >= 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
  if (bytes >= 1024) return `${Math.max(1, Math.round(bytes / 1024))} KB`
  return `${bytes} B`
}

/** Convert flat file list to tree */
function buildTreeFromFlatList(files: Array<{ name: string; rel_path?: string; is_dir?: boolean; size?: number }>): Node[] {
  const root: Node[] = []

  files.forEach((file) => {
    const rawPath = file.rel_path || file.name
    const parts = rawPath.split("/").filter(Boolean)
    let currentLevel = root

    parts.forEach((part, idx) => {
      const isLast = idx === parts.length - 1
      const isDir = isLast ? Boolean(file.is_dir) : true

      let existing = currentLevel.find((n) => n.name === part)
      if (!existing) {
        existing = {
          name: part,
          children: isDir ? [] : undefined,
          size: !isDir && file.size ? formatSize(file.size) : undefined,
        }
        currentLevel.push(existing)
      }
      if (existing.children) {
        currentLevel = existing.children
      }
    })
  })

  return root
}

/** Format tool execution into friendly Step label */
function formatToolStepLabel(name: string, args?: Record<string, unknown>): string {
  const cleanName = name.toLowerCase().replace(/^(agent_|tools_)/, "")
  if (cleanName === "read_file" || cleanName === "view_file") {
    const p = (args?.path || args?.AbsolutePath || args?.file || "") as string
    return p ? `Read ${p.split("/").pop() ?? p}` : "Read file"
  }
  if (cleanName === "write_file" || cleanName === "write_to_file") {
    const p = (args?.TargetFile || args?.path || args?.file || "") as string
    return p ? `Wrote ${p.split("/").pop() ?? p}` : "Wrote file"
  }
  if (cleanName === "replace_file_content" || cleanName === "edit_file") {
    const p = (args?.TargetFile || args?.path || "") as string
    return p ? `Edited ${p.split("/").pop() ?? p}` : "Edited file"
  }
  if (cleanName === "run_command" || cleanName === "execute_command") {
    const cmd = (args?.CommandLine || args?.command || "") as string
    return cmd ? `Ran ${cmd.slice(0, 48)}` : "Ran command"
  }
  if (cleanName === "grep_search" || cleanName === "search_web") {
    const q = (args?.Query || args?.query || args?.pattern || "") as string
    return q ? `Searched for “${q}”` : "Searched"
  }
  if (cleanName === "list_dir" || cleanName === "find_by_name") {
    const p = (args?.DirectoryPath || args?.SearchDirectory || args?.path || "") as string
    return p ? `Listed ${p.split("/").pop() ?? p}` : "Listed directory"
  }
  return name.replace(/_/g, " ")
}

/** Check if result looks like a git/unified diff */
function extractPatch(result?: string): string | undefined {
  if (!result) return undefined
  if (result.includes("@@ -") || result.startsWith("@@") || result.includes("diff --git")) {
    return result
  }
  return undefined
}

function Tree({
  nodes,
  open,
  chosen,
  query,
  onToggle,
  onChoose,
  depth = 0,
  under = "",
}: {
  nodes: Node[]
  open: Set<string>
  chosen: string
  query: string
  onToggle: (path: string) => void
  onChoose: (path: string) => void
  depth?: number
  under?: string
}) {
  return (
    <ul>
      {nodes
        .filter((node) => !query || matches(node, query))
        .map((node) => {
          const path = under + node.name
          const shown = !!node.children && (!!query || open.has(path))
          return (
            <li key={path}>
              <button
                type="button"
                aria-expanded={node.children ? shown : undefined}
                aria-current={chosen === path || undefined}
                onClick={() => (node.children ? onToggle(path) : onChoose(path))}
                style={{ paddingLeft: 6 + depth * INDENT }}
                className={cn(
                  "flex w-full items-center gap-1 rounded-sm py-[3px] pr-2 text-left transition-colors outline-none hover:bg-accent focus-visible:bg-accent",
                  chosen === path && "glass-chosen"
                )}
              >
                {node.children ? (
                  <ChevronRightIcon strokeWidth={1.5} className={cn("size-3.5 shrink-0 text-muted-foreground transition-transform", shown && "rotate-90")} />
                ) : (
                  <FileTextIcon strokeWidth={1.5} className="ml-0.5 size-3 shrink-0 text-muted-foreground/70" />
                )}
                <span className="min-w-0 flex-1 truncate text-[12.5px]">{node.name}</span>
                {node.size && <span className="shrink-0 text-[11px] text-muted-foreground/80 tabular-nums">{node.size}</span>}
              </button>
              {node.children && shown && (
                <Tree
                  nodes={node.children}
                  open={open}
                  chosen={chosen}
                  query={query}
                  onToggle={onToggle}
                  onChoose={onChoose}
                  depth={depth + 1}
                  under={`${path}/`}
                />
              )}
            </li>
          )
        })}
    </ul>
  )
}

function TaskBoard({ task, onStop }: { task: Task; onStop: () => void }) {
  return (
    <li className="glass-control rounded-md bg-(--glass-control) px-3 py-2.5">
      <div className="flex items-start gap-2">
        <div className="min-w-0 flex-1">
          <p className="line-clamp-2 text-[13px] leading-snug text-foreground/85">{task.title}</p>
          <p className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[12px] text-muted-foreground tabular-nums">
            <span>{task.kind}</span>
            <span>{task.state}</span>
            <span>{task.time}</span>
            {task.tokens && <span>{task.tokens}</span>}
          </p>
          {task.running && task.activity && <p className="status-shimmer mt-1 truncate font-mono text-[11px]">{task.activity}</p>}
        </div>
        {task.running && (
          <button
            type="button"
            aria-label={`Stop ${task.title}`}
            onClick={onStop}
            className="flex size-6 shrink-0 items-center justify-center rounded-sm text-muted-foreground transition-colors outline-none hover:bg-accent hover:text-foreground focus-visible:bg-accent"
          >
            <SquareIcon className="size-3" />
          </button>
        )}
      </div>
    </li>
  )
}

function IconButton({
  label,
  pressed,
  className,
  ...props
}: React.ComponentProps<"button"> & { label: string; pressed?: boolean }) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      aria-pressed={pressed}
      className={cn(
        "flex size-7 items-center justify-center rounded-sm text-muted-foreground transition-colors outline-none hover:bg-accent hover:text-foreground focus-visible:bg-accent [&_svg]:size-4",
        pressed && "text-foreground",
        className
      )}
      {...props}
    />
  )
}

export interface HextaAICanvasProps {
  initialSidebarOpen?: boolean
  isDrawer?: boolean
  onCloseDrawer?: () => void
}

export function HextaAICanvas({
  initialSidebarOpen = true,
  isDrawer = false,
  onCloseDrawer,
}: HextaAICanvasProps) {
  const {
    sessions,
    activeSessionId,
    switchSession,
    createNewChat,
    deleteSession,
    messages,
    models,
    activeModel,
    selectModel,
    thinkingLevel,
    setThinkingLevel,
    status,
    setAutonomyMode,
    isProcessing,
    sendPrompt,
    confirmApproval,
    abort,
    contextUsage,
    slashCommands,
    refreshEngine,
    providers,
    loginProviders,
    oauthPrompt,
    startOAuthLogin,
    submitOAuthInput,
    saveProviders,
  } = useCopilot()

  const [sidebar, setSidebar] = React.useState(initialSidebarOpen)
  const [finding, setFinding] = React.useState<string | null>(null)
  const [value, setValue] = React.useState("")
  const [attached, setAttached] = React.useState<string[]>([])
  const [panels, setPanels] = React.useState({ files: !isDrawer, tasks: !isDrawer })
  const [drawer, setDrawer] = React.useState<"sessions" | "panels" | null>(null)
  const [isSettingsOpen, setIsSettingsOpen] = React.useState(false)

  // Files state
  const [tree, setTree] = React.useState<Node[]>(DEFAULT_TREE)
  const [open, setOpen] = React.useState(() => new Set(["src", "src/features", "research"]))
  const [chosen, setChosen] = React.useState("src/features/copilot/HextaAICanvas.tsx")
  const [looking, setLooking] = React.useState("")
  const [turning, setTurning] = React.useState(false)
  const picker = React.useRef<HTMLInputElement>(null)

  // Ended tasks folding
  const [ended, setEnded] = React.useState(true)

  const thread = React.useRef<HTMLDivElement>(null)
  const grown = React.useRef(false)

  // Fetch real workspace files from active app
  const loadWorkspaceFiles = React.useCallback(async () => {
    try {
      setTurning(true)
      const bootRes = await appsAndDockerApi.bootstrap()
      const apps = (bootRes.data as any)?.apps || []
      const appId = apps[0]?.id
      if (appId) {
        const listRes = await appsAndDockerApi.filesList(appId, "", true)
        if (listRes.ok && Array.isArray(listRes.data) && listRes.data.length > 0) {
          const generatedTree = buildTreeFromFlatList(listRes.data)
          if (generatedTree.length > 0) {
            setTree(generatedTree)
          }
        }
      }
    } catch {
      // fallback to default tree
    } finally {
      window.setTimeout(() => setTurning(false), 500)
    }
  }, [])

  React.useEffect(() => {
    loadWorkspaceFiles()
  }, [loadWorkspaceFiles])

  // Scroll to bottom as thread grows
  React.useEffect(() => {
    if (grown.current) {
      thread.current?.scrollTo({ top: thread.current.scrollHeight, behavior: "smooth" })
    }
    grown.current = true
  }, [messages, isProcessing])

  // If both panels closed, close panel drawer
  React.useEffect(() => {
    if (!panels.files && !panels.tasks) {
      setDrawer((now) => (now === "panels" ? null : now))
    }
  }, [panels])

  // Keyboard shortcut Cmd+K for search, Cmd+N for new chat
  React.useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault()
        setFinding("")
      }
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "n") {
        e.preventDefault()
        createNewChat()
      }
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [createNewChat])

  // Current session object
  const currentSession = sessions.find((s) => s.id === activeSessionId) || sessions[0] || {
    id: "default",
    title: "NextDeploy Copilot",
    createdAt: Date.now(),
    updatedAt: Date.now(),
    messageCount: 0,
  }

  // Session groups
  const groups = React.useMemo(() => {
    const list = sessions.map((s) => s.group || "Conversations")
    return [...new Set(list)]
  }, [sessions])

  const found = finding?.trim().toLowerCase() ?? ""

  // Aggregate background tasks from all tool executions in messages
  const allTasks: Task[] = React.useMemo(() => {
    const tasks: Task[] = []
    messages.forEach((msg) => {
      if (msg.toolCalls) {
        msg.toolCalls.forEach((tc) => {
          const isRunning = tc.status === "running"
          const elapsedSec = Math.max(
            1,
            Math.round(((tc.endedAt || Date.now()) - tc.startedAt) / 1000)
          )
          const timeStr = elapsedSec < 60 ? `${elapsedSec}s` : `${Math.floor(elapsedSec / 60)}m ${elapsedSec % 60}s`
          const cmdArg = (tc.args?.CommandLine || tc.args?.command || tc.args?.path || "") as string

          tasks.push({
            title: formatToolStepLabel(tc.name, tc.args),
            kind: tc.name.includes("command") ? "Command" : "Tool",
            state: isRunning ? "Running" : tc.status === "completed" ? "Completed" : "Failed",
            time: timeStr,
            activity: cmdArg ? String(cmdArg).slice(0, 60) : undefined,
            running: isRunning,
          })
        })
      }
    })
    return tasks.reverse()
  }, [messages])

  const runningTasks = allTasks.filter((t) => t.running)
  const overTasks = allTasks.filter((t) => !t.running)

  // Send message
  const handleSend = (text: string) => {
    if (!text.trim()) return
    sendPrompt(text, attached)
    setValue("")
    setAttached([])
  }

  // Toggle panel
  const togglePanel = (name: "files" | "tasks") => {
    if (wide("(min-width: 64rem)")) {
      setPanels((now) => ({ ...now, [name]: !now[name] }))
      return
    }
    const out = drawer === "panels" && panels[name]
    setPanels((now) => ({ ...now, [name]: true }))
    setDrawer(out ? null : "panels")
  }

  const selectedFileName = chosen.split("/").pop() ?? ""

  // Models list for ComposerPick
  const modelOptions = React.useMemo(() => {
    if (!models || models.length === 0) {
      return [{ value: "default", label: "model-default" }]
    }
    return models.map((m) => ({
      value: m.id,
      label: m.name || m.id,
      hint: m.provider ? `Provider: ${m.provider}` : undefined,
    }))
  }, [models])

  return (
    <div
      data-slot="agent-workspace"
      className="flex h-full w-full overflow-hidden bg-background text-[13px] text-foreground select-none"
    >
      {/* Drawer backdrop for mobile screens */}
      {drawer && (
        <button
          type="button"
          aria-label="Close"
          onClick={() => setDrawer(null)}
          className={cn("fixed inset-0 z-20 bg-black/45", drawer === "sessions" ? "md:hidden" : "lg:hidden")}
        />
      )}

      {/* Sessions Drawer / Sidebar */}
      {(sidebar || drawer === "sessions") && (
        <aside
          className={cn(
            "w-[216px] shrink-0 flex-col border-r bg-sidebar z-30",
            sidebar ? "md:flex" : "md:hidden",
            drawer === "sessions"
              ? "max-md:fixed max-md:inset-y-0 max-md:left-0 max-md:z-40 max-md:flex max-md:w-[272px] max-md:max-w-[85vw] max-md:bg-(--popover)"
              : "max-md:hidden"
          )}
        >
          {/* Header */}
          <div className="flex h-12 items-center gap-[5px] pr-2 pl-[11px]">
            <span className="glass-chosen flex size-5 items-center justify-center rounded-[6px] text-[10px] font-semibold text-primary">
              ND
            </span>
            <span className="text-sm font-medium truncate">Copilot Sessions</span>
            <IconButton
              label="Hide sessions"
              className="ml-auto"
              onClick={() => (wide("(min-width: 48rem)") ? setSidebar(false) : setDrawer(null))}
            >
              <PanelLeftIcon />
            </IconButton>
          </div>

          {/* Action Row */}
          <div className="flex flex-col gap-px px-1.5">
            {finding === null ? (
              <PanelRow onClick={() => setFinding("")}>
                <SearchIcon /> Search
                <span className="ml-auto flex gap-1">
                  <Kbd>⌘</Kbd>
                  <Kbd>K</Kbd>
                </span>
              </PanelRow>
            ) : (
              <label className="flex h-7 items-center gap-2 rounded-sm bg-accent px-2 text-[12.5px] [&_svg]:size-3.5 [&_svg]:shrink-0 [&_svg]:text-muted-foreground">
                <SearchIcon />
                <input
                  autoFocus
                  value={finding}
                  onChange={(event) => setFinding(event.target.value)}
                  onKeyDown={(event) => event.key === "Escape" && setFinding(null)}
                  placeholder="Search sessions"
                  className="min-w-0 flex-1 bg-transparent outline-none placeholder:text-muted-foreground"
                />
                <button
                  type="button"
                  aria-label="Close search"
                  onClick={() => setFinding(null)}
                  className="rounded-sm outline-none hover:text-foreground"
                >
                  <XIcon />
                </button>
              </label>
            )}
            <PanelRow
              onClick={() => {
                createNewChat()
                setDrawer(null)
              }}
            >
              <PlusIcon /> New chat
            </PanelRow>
          </div>

          {/* Sessions List */}
          <div className="min-h-0 flex-1 overflow-y-auto [scrollbar-width:thin]">
            {groups.map((group) => {
              const rows = sessions.filter(
                (each) =>
                  (each.group || "Conversations") === group &&
                  (!found || each.title.toLowerCase().includes(found))
              )
              return rows.length ? (
                <React.Fragment key={group}>
                  <p className="px-3.5 pt-5 pb-1 text-xs text-muted-foreground">{group}</p>
                  <div className="flex flex-col gap-px px-1.5">
                    {rows.map((each) => (
                      <div key={each.id} className="group/row relative flex items-center">
                        <PanelRow
                          chosen={each.id === activeSessionId}
                          onClick={() => {
                            switchSession(each.id)
                            setDrawer(null)
                          }}
                          className="pr-6"
                        >
                          <span className="truncate">{each.title}</span>
                        </PanelRow>
                        {sessions.length > 1 && (
                          <button
                            type="button"
                            aria-label="Delete session"
                            onClick={(e) => {
                              e.stopPropagation()
                              deleteSession(each.id)
                            }}
                            className="absolute right-1 hidden size-5 items-center justify-center rounded text-muted-foreground/60 hover:text-destructive group-hover/row:flex"
                          >
                            <Trash2 className="size-3" />
                          </button>
                        )}
                      </div>
                    ))}
                  </div>
                </React.Fragment>
              ) : null
            })}
          </div>

          {/* Footer User Info */}
          <div className="flex items-center gap-2 border-t py-3 pr-3.5 pl-[11px]">
            <span className="flex size-5 items-center justify-center rounded-full bg-muted text-[10px] font-medium text-foreground">
              A
            </span>
            <span className="text-xs truncate font-medium">Administrator</span>
            <IconButton
              label="Provider Settings"
              className="ml-auto size-6"
              onClick={() => setIsSettingsOpen(true)}
            >
              <Settings2 className="size-3.5" />
            </IconButton>
          </div>
        </aside>
      )}

      {/* Main Interactive Thread */}
      <main className="flex min-w-0 flex-1 flex-col select-text">
        {/* Header Bar */}
        <div className="flex h-12 shrink-0 items-center gap-1.5 px-3 border-b border-border/40">
          <IconButton
            label="Show sessions"
            className={cn(sidebar && "md:hidden")}
            onClick={() => (wide("(min-width: 48rem)") ? setSidebar(true) : setDrawer("sessions"))}
          >
            <PanelLeftIcon />
          </IconButton>

          <span className="min-w-0 truncate px-1 text-sm font-medium">
            {currentSession.title}
          </span>

          <span className="ml-auto flex items-center gap-1">
            <IconButton
              label="Provider Settings"
              onClick={() => setIsSettingsOpen(true)}
              className="text-muted-foreground"
            >
              <Settings2 />
            </IconButton>
            <IconButton
              label="Background tasks"
              pressed={panels.tasks}
              onClick={() => togglePanel("tasks")}
            >
              <ListChecksIcon />
            </IconButton>
            <IconButton
              label="Files"
              pressed={panels.files}
              onClick={() => togglePanel("files")}
            >
              <FolderIcon />
            </IconButton>
            {isDrawer && onCloseDrawer && (
              <IconButton label="Close Copilot" onClick={onCloseDrawer}>
                <XIcon />
              </IconButton>
            )}
          </span>
        </div>

        {/* Message Thread Scroller */}
        <div
          ref={thread}
          className="flex min-h-0 flex-1 justify-center overflow-y-auto px-4 pt-5 pb-6 [scrollbar-width:thin] md:px-6"
        >
          <div className="flex h-fit w-full max-w-[620px] flex-col gap-4">
            {messages.map((turn: CopilotMessage) => (
              <React.Fragment key={turn.id}>
                {/* User Message */}
                {turn.role === "user" && turn.content && (
                  <div className="flex flex-col items-end gap-1.5">
                    {!!turn.attached?.length && (
                      <div className="flex flex-wrap justify-end gap-1">
                        {turn.attached.map((name) => (
                          <Tag key={name} icon={<FileTextIcon />}>
                            {name}
                          </Tag>
                        ))}
                      </div>
                    )}
                    <div className="glass-control rounded-2xl bg-secondary px-3.5 py-2 text-sm leading-relaxed max-w-[85%] whitespace-pre-wrap">
                      {turn.content}
                    </div>
                  </div>
                )}

                {/* Assistant Message */}
                {turn.role === "assistant" && (
                  <div className="flex flex-col gap-3 text-sm leading-relaxed">
                    {/* Tool Steps */}
                    {!!turn.toolCalls?.length && (
                      <StepGroup
                        steps={turn.toolCalls.map((tc: ToolExecution) => ({
                          id: tc.id,
                          label: formatToolStepLabel(tc.name, tc.args),
                          status:
                            tc.status === "completed"
                              ? "done"
                              : tc.status === "failed"
                              ? "error"
                              : "running",
                          detail: tc.args ? JSON.stringify(tc.args, null, 2) : undefined,
                          detailLabel: "Parameters",
                          output: tc.result,
                          patch: extractPatch(tc.result),
                        }))}
                        defaultOpen
                      />
                    )}

                    {/* Markdown Answer */}
                    {turn.content ? (
                      <MarkdownContent content={turn.content} isStreaming={turn.isStreaming} />
                    ) : null}

                    {/* Interactive Approval / Question Card */}
                    {turn.approval && (
                      <AskCard
                        eyebrow="Permission Required"
                        question={turn.approval.message || turn.approval.title}
                        options={["Approve & Execute", "Reject / Cancel"]}
                        onAnswer={(opt) => {
                          const isConfirmed = opt.toLowerCase().includes("approve")
                          confirmApproval(turn.approval!.id, isConfirmed)
                        }}
                      />
                    )}
                  </div>
                )}
              </React.Fragment>
            ))}

            {/* Live Turn Status */}
            {isProcessing && (
              <TurnStatus
                activity="thinking"
                label={runningTasks[0]?.title || "Thinking"}
                since={Date.now()}
                tokens={contextUsage?.tokens}
              />
            )}

            {/* Empty Thread Prompt */}
            {!messages.length && (
              <p className="pt-24 text-center text-sm text-muted-foreground">
                Ask NextDeploy Copilot anything to begin.
              </p>
            )}
          </div>
        </div>

        {/* Composer Dock */}
        <div className="flex shrink-0 justify-center px-4 pb-4 md:px-6">
          <div className="w-full max-w-[620px]">
            <Composer
              textareaProps={{ "aria-label": "Message" }}
              value={value}
              onValueChange={setValue}
              onSubmit={handleSend}
              running={isProcessing}
              onStop={abort}
              slashCommands={slashCommands}
              onBackspaceAtStart={() => {
                if (!attached.length) return false
                setAttached((all) => all.slice(0, -1))
                return true
              }}
              placeholder="Ask Copilot, run commands, or type / for skills…"
              leading={
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={`Attach ${selectedFileName}`}
                  title={selectedFileName ? `Attach ${selectedFileName}` : "Select a file to attach"}
                  disabled={!selectedFileName || attached.includes(selectedFileName)}
                  onClick={() => {
                    if (selectedFileName && !attached.includes(selectedFileName)) {
                      setAttached((all) => [...all, selectedFileName])
                    }
                  }}
                  className="shrink-0 text-muted-foreground"
                >
                  <PlusIcon className="size-4" />
                </Button>
              }
              tags={
                attached.length ? (
                  <>
                    {attached.map((name) => (
                      <Tag
                        key={name}
                        className="mb-0.5"
                        icon={<FileTextIcon />}
                        onRemove={() => setAttached((all) => all.filter((each) => each !== name))}
                      >
                        {name}
                      </Tag>
                    ))}
                  </>
                ) : undefined
              }
            />

            <ComposerFooter>
              <ComposerPick
                label="Mode"
                align="start"
                value={status.autonomyMode || "safe"}
                options={MODES}
                onValueChange={(m) => setAutonomyMode(m as any)}
              />

              <ComposerFooterEnd>
                <ComposerPick
                  label="Model"
                  value={activeModel?.id || models[0]?.id || "default"}
                  options={modelOptions}
                  onValueChange={(selectedId) => {
                    const foundModel = models.find((m) => m.id === selectedId)
                    if (foundModel) {
                      selectModel(foundModel.provider, foundModel.id)
                    }
                  }}
                />

                <ComposerPick
                  label="Effort"
                  value={thinkingLevel || "medium"}
                  options={EFFORTS}
                  shown={`Effort · ${EFFORTS.find((e) => e.value === thinkingLevel)?.label || "Medium"}`}
                  onValueChange={(eff) => setThinkingLevel(eff as any)}
                />

                <ContextRing
                  used={contextUsage?.tokens ?? 86300}
                  window={contextUsage?.contextWindow ?? 200000}
                />
              </ComposerFooterEnd>
            </ComposerFooter>
          </div>
        </div>
      </main>

      {/* Right Collapsible Panels (Files & Background Tasks) */}
      {(panels.files || panels.tasks) && (
        <aside
          className={cn(
            "w-[272px] shrink-0 flex-col gap-3 py-3 pr-3 lg:flex z-20",
            drawer === "panels"
              ? "max-lg:fixed max-lg:inset-y-0 max-lg:right-0 max-lg:z-40 max-lg:flex max-lg:w-[300px] max-lg:max-w-[88vw] max-lg:border-l max-lg:bg-background max-lg:pl-3"
              : "max-lg:hidden"
          )}
        >
          {/* Files Panel */}
          {panels.files && (
            <Panel
              title="Files"
              className={panels.tasks ? "max-h-[55%] shrink-0" : "min-h-0 flex-1"}
              onClose={() => setPanels((now) => ({ ...now, files: false }))}
              actions={
                <span className="flex items-center">
                  <IconButton
                    label="New folder"
                    className="size-6 [&_svg]:size-3.5"
                    onClick={() => {
                      const taken = new Set(tree.map((node) => node.name))
                      let name = "untitled"
                      for (let n = 2; taken.has(name); n++) name = `untitled ${n}`
                      setTree((all) => [...all, { name, children: [] }])
                    }}
                  >
                    <FolderPlusIcon />
                  </IconButton>

                  <IconButton
                    label="Upload"
                    className="size-6 [&_svg]:size-3.5"
                    onClick={() => picker.current?.click()}
                  >
                    <UploadIcon />
                  </IconButton>
                  <input
                    ref={picker}
                    type="file"
                    hidden
                    onChange={(event) => {
                      const picked = event.target.files?.[0]
                      if (picked) {
                        setTree((all) => [
                          ...all.filter((node) => node.name !== picked.name),
                          { name: picked.name, size: formatSize(picked.size) },
                        ])
                      }
                      event.target.value = ""
                    }}
                  />

                  <IconButton
                    label="Refresh"
                    className="size-6 [&_svg]:size-3.5"
                    onClick={loadWorkspaceFiles}
                  >
                    <RefreshCwIcon className={cn(turning && "animate-spin")} />
                  </IconButton>
                </span>
              }
            >
              <label className="mx-1.5 mb-1 flex h-7 items-center gap-1.5 rounded-sm px-1.5 text-[12.5px] text-muted-foreground focus-within:bg-accent">
                <SearchIcon className="size-3.5 shrink-0" />
                <input
                  value={looking}
                  onChange={(event) => setLooking(event.target.value)}
                  placeholder="Search files and folders"
                  className="min-w-0 flex-1 bg-transparent text-foreground outline-none placeholder:text-muted-foreground"
                />
              </label>
              <div className="min-h-0 overflow-y-auto px-1.5 pb-2 [scrollbar-width:thin]">
                <Tree
                  nodes={tree}
                  open={open}
                  chosen={chosen}
                  query={looking.trim().toLowerCase()}
                  onChoose={(path) => {
                    setChosen(path)
                    const fName = path.split("/").pop() ?? path
                    if (fName && !attached.includes(fName)) {
                      setAttached((all) => [...all, fName])
                    }
                  }}
                  onToggle={(path) =>
                    setOpen((now) => {
                      const next = new Set(now)
                      if (!next.delete(path)) next.add(path)
                      return next
                    })
                  }
                />
              </div>
            </Panel>
          )}

          {/* Background Tasks Panel */}
          {panels.tasks && (
            <Panel
              title="Background tasks"
              className="min-h-0 flex-1"
              onClose={() => setPanels((now) => ({ ...now, tasks: false }))}
            >
              <div className="min-h-0 overflow-y-auto [scrollbar-width:thin]">
                <p className="px-3 pb-1 text-[11.5px] text-muted-foreground/80">
                  Running{runningTasks.length ? "" : " · none"}
                </p>
                <ul className="flex flex-col gap-1.5 px-1.5">
                  {runningTasks.map((task, idx) => (
                    <TaskBoard
                      key={`${task.title}-${idx}`}
                      task={task}
                      onStop={abort}
                    />
                  ))}
                </ul>

                <button
                  type="button"
                  aria-expanded={ended}
                  onClick={() => setEnded((now) => !now)}
                  className="mt-2 ml-1.5 flex items-center gap-1 rounded-sm px-1.5 py-1 text-[11.5px] text-muted-foreground/80 transition-colors outline-none hover:bg-accent hover:text-foreground focus-visible:bg-accent"
                >
                  Ended {overTasks.length}{" "}
                  <ChevronRightIcon className={cn("size-3 transition-transform", ended && "rotate-90")} />
                </button>

                {ended && (
                  <ul className="flex flex-col gap-1.5 px-1.5 pt-1 pb-2">
                    {overTasks.map((task, idx) => (
                      <TaskBoard key={`${task.title}-${idx}`} task={task} onStop={() => {}} />
                    ))}
                  </ul>
                )}
              </div>
            </Panel>
          )}
        </aside>
      )}

      {/* Provider Auth & BYOK Modal */}
      {isSettingsOpen && (
        <ProviderAuthModal
          isOpen={isSettingsOpen}
          onClose={() => setIsSettingsOpen(false)}
          providers={providers}
          loginProviders={loginProviders}
          oauthPrompt={oauthPrompt}
          onStartOAuth={startOAuthLogin}
          onSubmitOAuthInput={submitOAuthInput}
          onSaveKeys={saveProviders}
        />
      )}
    </div>
  )
}

export default HextaAICanvas
