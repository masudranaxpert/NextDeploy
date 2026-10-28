import * as React from "react"
import {
  ChevronRightIcon,
  FileTextIcon,
  FolderIcon,
    ListChecksIcon,
  PanelLeftIcon,
  PlusIcon,
  RefreshCwIcon,
  SearchIcon,
  SquareIcon,
  XIcon,
  ArrowUp,
  Settings2,
  Trash2,
  ShrinkIcon,
  ZapIcon,
  MoreHorizontalIcon,
  Minimize2Icon,
  Maximize2Icon,
  GitForkIcon,
  ArrowRightLeftIcon,
  DownloadIcon,
} from "lucide-react"

import { cn } from "@/lib/utils"
import { AskCard } from "@/components/ui/ask-card"
import { Button } from "@/components/ui/button"
import { Composer, ComposerFooter, ComposerFooterEnd, ComposerPick } from "@/components/ui/composer"
import { ContextMeter } from "./ContextMeter"
import { Kbd } from "@/components/ui/kbd"
import { Panel, PanelRow } from "@/components/ui/panel"
import { Tag } from "@/components/ui/tag"
import { StepGroup, type Step } from "@/components/ui/tool-steps"
import { TurnStatus } from "@/components/ui/turn-status"
import { MarkdownContent } from "./MarkdownContent"
import { ProviderAuthModal } from "./ProviderAuthModal"
import { AgentTodos } from "./AgentTodos"
import { toolToStep } from "./toolSteps"
import { AskDialog } from "./AskDialog"
import { ThinkingBlock } from "./ThinkingBlock"
import { ShadcnSelect } from "@/components/ui/ShadcnSelect"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { useCopilot } from "./useCopilot"
import type { ToolExecution, CopilotMessage } from "./types"
import { requestJson } from "@/lib/api/http"

// Project file node
type Node = { name: string; size?: string; children?: Node[] }

// Background task
type Task = {
  id: string
  title: string
  kind: string
  state: string
  time: string
  tokens?: string
  activity?: string
  running?: boolean
}

// omp's own tools.approvalMode values.
const MODES = [
  { value: "always-ask", label: "Ask every time", hint: "Confirm every tool call" },
  { value: "always-ask-cached", label: "Ask once", hint: "Confirm a tool or path once, then remember" },
  { value: "write", label: "Ask before changes", hint: "Reads are free; edits and commands need a yes" },
  { value: "yolo", label: "Autonomous", hint: "Never asks; carries on by itself" },
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
  expanded?: boolean
  onToggleExpand?: () => void
  initialSidebarOpen?: boolean
  isDrawer?: boolean
  onCloseDrawer?: () => void
}

export function HextaAICanvas({
  initialSidebarOpen = true,
  isDrawer = false,
  onCloseDrawer,
  expanded,
  onToggleExpand,
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
    availableThinkingLevels,
    setThinkingLevel,
    workDir,
    setWorkDir,
    engineSettings,
    setEngineSetting,
    compactContext,
    forkChat,
    handoffChat,
    exportChat,
    notice,
    dismissNotice,
    uiPrompt,
    submitUiInput,
    removeProvider,
    stats,
    askPrompt,
    submitAnswers,
    uploadFile,
    todos,
    subagents,
    cancelSubagent,
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

  const [sidebar, setSidebar] = React.useState(() => initialSidebarOpen && wide("(min-width: 48rem)"))
  const [finding, setFinding] = React.useState<string | null>(null)
  const [value, setValue] = React.useState("")
  const [attached, setAttached] = React.useState<string[]>([])
  const [images, setImages] = React.useState<Array<{ name: string; data: string; mimeType: string }>>([])
  const [uploading, setUploading] = React.useState(false)
  const attachInput = React.useRef<HTMLInputElement>(null)

  // Local files: images travel inside the prompt (omp image content), everything
  // else is stored with the conversation and referenced with @path.
  const attachLocal = async (files: FileList | File[]) => {
    setUploading(true)
    try {
      for (const file of Array.from(files)) {
        if (file.type.startsWith("image/")) {
          const data = await new Promise<string>((resolve, reject) => {
            const r = new FileReader()
            r.onload = () => resolve(String(r.result).split(",")[1] || "")
            r.onerror = () => reject(r.error)
            r.readAsDataURL(file)
          })
          setImages((all) => [...all, { name: file.name, data, mimeType: file.type }])
        } else {
          const path = await uploadFile(file)
          if (path) setAttached((all) => (all.includes(path) ? all : [...all, path]))
        }
      }
    } finally {
      setUploading(false)
    }
  }
  const [panels, setPanels] = React.useState(() => ({ files: !isDrawer && wide("(min-width: 80rem)"), tasks: !isDrawer && wide("(min-width: 80rem)") }))
  const [drawer, setDrawer] = React.useState<"sessions" | "panels" | null>(null)
  const [isSettingsOpen, setIsSettingsOpen] = React.useState(false)

  // Files state
  const [tree, setTree] = React.useState<Node[]>([])
  const [open, setOpen] = React.useState<Set<string>>(() => new Set())
  const [chosen, setChosen] = React.useState("")
  const [looking, setLooking] = React.useState("")
  const [turning, setTurning] = React.useState(false)

  // Ended tasks folding
  const [ended, setEnded] = React.useState(true)

  const thread = React.useRef<HTMLDivElement>(null)
  const grown = React.useRef(false)

  // The file tree shows exactly the directory omp is running in.
  const loadWorkspaceFiles = React.useCallback(async () => {
    setTurning(true)
    const res = await requestJson<{ files: Array<{ name: string; rel_path: string; is_dir: boolean; size: number }> }>(
      "/api/v1/copilot/files"
    )
    if (res.ok && res.data) setTree(buildTreeFromFlatList(res.data.files || []))
    window.setTimeout(() => setTurning(false), 300)
  }, [])

  React.useEffect(() => {
    void loadWorkspaceFiles()
  }, [workDir?.kind, workDir?.appId, workDir?.path, loadWorkspaceFiles])

  // The agent may have created or changed files: reload when a turn ends.
  const wasProcessing = React.useRef(false)
  React.useEffect(() => {
    if (wasProcessing.current && !isProcessing) void loadWorkspaceFiles()
    wasProcessing.current = isProcessing
  }, [isProcessing, loadWorkspaceFiles])

  // Scroll to bottom as thread grows
  const seen = React.useRef(0)
  React.useEffect(() => {
    const el = thread.current
    if (el) {
      // A whole conversation appearing at once (open / reload) jumps to the end; streaming glides.
      const jumped = messages.length - seen.current > 2 || !grown.current
      el.scrollTo({ top: el.scrollHeight, behavior: jumped ? "auto" : "smooth" })
    }
    seen.current = messages.length
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
  const currentSession = sessions.find((s) => s.id === activeSessionId) || {
    id: "default",
    title: "New conversation",
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

  // Background tasks are omp's own subagents (native RPC), not UI-derived.
  const allTasks: Task[] = React.useMemo(() => {
    return [...subagents].reverse().map((sa) => {
      const running = sa.status === "started" || sa.status === "running"
      const secs = Math.max(1, Math.round((Date.now() - sa.startedAt) / 1000))
      return {
        id: sa.id,
        title: sa.description || sa.agent,
        kind: sa.detached ? "Background job" : "Subagent",
        state: running ? "Running" : sa.status.charAt(0).toUpperCase() + sa.status.slice(1),
        time: secs < 60 ? `${secs}s` : `${Math.floor(secs / 60)}m ${secs % 60}s`,
        activity: running ? sa.agent : undefined,
        running,
      }
    })
  }, [subagents, isProcessing])

  const runningTasks = allTasks.filter((t) => t.running)
  const overTasks = allTasks.filter((t) => !t.running)

  // Queued message state when agent is busy
  const [queuedMessage, setQueuedMessage] = React.useState<{
    text: string
    attached: string[]
    displaySnippet: string
  } | null>(null)

  const queuedRef = React.useRef<{
    text: string
    attached: string[]
    displaySnippet: string
  } | null>(null)
  queuedRef.current = queuedMessage

  // Automatically dispatch queued message when current turn completes
  React.useEffect(() => {
    if (!isProcessing && queuedRef.current) {
      const q = queuedRef.current
      setQueuedMessage(null)
      sendPrompt(q.text, q.attached)
    }
  }, [isProcessing, sendPrompt])

  const handleSendQueuedNow = async () => {
    if (!queuedRef.current) return
    const q = queuedRef.current
    setQueuedMessage(null)
    await abort()
    setTimeout(() => {
      sendPrompt(q.text, q.attached)
    }, 120)
  }

  const handleCancelQueued = () => {
    if (!queuedRef.current) return
    const q = queuedRef.current
    setValue(q.displaySnippet)
    setQueuedMessage(null)
  }

  // Send message or queue if agent is busy
  const handleSend = (text: string) => {
    if (!text.trim() && images.length === 0) return
    // omp really runs in the selected directory (see workDir), so no prompt rewriting is needed.
    sendPrompt(text, attached, images)
    setValue("")
    setAttached([])
    setImages([])
  }

  const workDirValue = workDir?.kind === "site" ? `site:${workDir.appId}` : workDir?.kind || "root"
  const workDirOptions = React.useMemo(
    () => [
      { value: "root", label: "Workspace root", sublabel: "All sites" },
      { value: "temp", label: "Temp folder", sublabel: "Scratch dir, deleted on change" },
      ...(workDir?.sites || []).map((id) => ({ value: `site:${id}`, label: id, sublabel: "Site workspace" })),
    ],
    [workDir?.sites]
  )
  const onWorkDirChange = (v: string) => {
    if (v.startsWith("site:")) void setWorkDir("site", v.slice(5))
    else void setWorkDir(v === "temp" ? "temp" : "root")
  }

  // Slash commands (with subcommands) for the composer popup
  const flattenedSlashCommands = React.useMemo(() => {
    const list: Array<{ name: string; description?: string; hint?: string; source?: string }> = []
    slashCommands.forEach((cmd) => {
      list.push({
        name: cmd.name,
        description: cmd.description,
        hint: cmd.input?.hint,
        source: cmd.source,
      })
      if (cmd.subcommands?.length) {
        cmd.subcommands.forEach((sub) => {
          list.push({
            name: `${cmd.name} ${sub.name}`,
            description: sub.description || cmd.description,
            hint: sub.usage,
            source: cmd.source,
          })
        })
      }
    })
    return list
  }, [slashCommands])

  // Thinking levels come from omp for the active model; fall back to a static set.
  const effortOptions = React.useMemo(() => {
    if (activeModel && activeModel.reasoning === false) return []
    if (availableThinkingLevels && availableThinkingLevels.length > 0) {
      const hints: Record<string, string> = {
        off: "No reasoning steps",
        minimal: "Barely any reasoning",
        low: "Quick, concise thought",
        medium: "Balanced exploration",
        high: "Deeper problem solving",
        xhigh: "Very deep reasoning",
        max: "Exhaustive reasoning chain",
        inherit: "Inherit provider default",
        auto: "Automatic reasoning budget",
      }
      return availableThinkingLevels.map((lvl) => {
        const cap = lvl.charAt(0).toUpperCase() + lvl.slice(1)
        return { value: lvl, label: cap, hint: hints[lvl] || `${cap} reasoning effort` }
      })
    }
    return []
  }, [availableThinkingLevels, activeModel])

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

  const selectedFileName = chosen

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
            <span className="text-sm font-medium truncate">ND Sessions</span>
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
                        {sessions.length > 0 && (
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
            <span className="text-xs truncate font-medium">admin</span>
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
        <div className="flex min-h-12 shrink-0 flex-wrap items-center gap-x-1.5 gap-y-1 border-b border-border/40 px-3 py-1.5">
          <IconButton
            label="Show sessions"
            className={cn(sidebar && "md:hidden")}
            onClick={() => (wide("(min-width: 48rem)") ? setSidebar(true) : setDrawer("sessions"))}
          >
            <PanelLeftIcon />
          </IconButton>

          <span className="min-w-[4.5rem] max-w-[16rem] flex-1 truncate px-1 text-sm font-medium" title={currentSession.title}>
            {currentSession.title}
          </span>

          {/* Working directory omp runs in */}
          <div className="ml-1 flex shrink-0 items-center gap-1.5 border-l border-border/40 pl-2 max-sm:order-last max-sm:ml-0 max-sm:w-full max-sm:border-l-0 max-sm:pl-0">
            <span className="hidden text-[11px] whitespace-nowrap text-muted-foreground xl:inline">Run in</span>
            <ShadcnSelect
              title={workDir?.path || "Working directory"}
              value={workDirValue}
              onChange={onWorkDirChange}
              options={workDirOptions}
              disabled={isProcessing}
              className="w-full sm:w-36 lg:w-44"
              menuClassName="min-w-64"
            />
          </div>

          <span className="ml-auto flex items-center gap-1">
            <Popover>
              <PopoverTrigger
                aria-label="Conversation actions"
                title="Conversation actions"
                className="flex size-7 items-center justify-center rounded-sm text-muted-foreground transition-colors outline-none hover:bg-accent hover:text-foreground focus-visible:bg-accent"
              >
                <MoreHorizontalIcon className="size-4" />
              </PopoverTrigger>
              <PopoverContent align="end" className="w-60 p-1">
                {[
                  {
                    key: "fast",
                    icon: <ZapIcon className="size-4" />,
                    label: "Fast mode",
                    hint: engineSettings.fastMode ? "On" : "Off",
                    run: () => setEngineSetting("fast_mode", !engineSettings.fastMode),
                  },
                  { key: "compact", icon: <ShrinkIcon className="size-4" />, label: "Compact context", run: () => compactContext(), busy: true },
                  { key: "fork", icon: <GitForkIcon className="size-4" />, label: "Fork conversation", run: () => forkChat(), busy: true },
                  { key: "handoff", icon: <ArrowRightLeftIcon className="size-4" />, label: "Hand off to a fresh chat", run: () => handoffChat(), busy: true },
                  { key: "export", icon: <DownloadIcon className="size-4" />, label: "Export as HTML", run: () => exportChat() },
                ].map((item) => (
                  <button
                    key={item.key}
                    type="button"
                    disabled={item.busy && isProcessing}
                    onClick={() => void item.run()}
                    className="flex w-full items-center gap-2.5 rounded-sm px-2.5 py-2 text-left text-[13px] outline-none transition-colors hover:bg-accent focus-visible:bg-accent disabled:pointer-events-none disabled:opacity-50"
                  >
                    <span className="text-muted-foreground">{item.icon}</span>
                    <span className="flex-1">{item.label}</span>
                    {item.hint && <span className="text-[11.5px] text-muted-foreground">{item.hint}</span>}
                  </button>
                ))}
              </PopoverContent>
            </Popover>
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
            {isDrawer && onToggleExpand && (
              <IconButton label={expanded ? "Restore size" : "Expand"} onClick={onToggleExpand} className="max-sm:hidden">
                {expanded ? <Minimize2Icon /> : <Maximize2Icon />}
              </IconButton>
            )}
            {isDrawer && onCloseDrawer && (
              <IconButton label="Close NextDeploy AI" onClick={onCloseDrawer}>
                <XIcon />
              </IconButton>
            )}
          </span>
        </div>

        {/* Message Thread Scroller */}
        <div
          ref={thread}
          className="flex min-h-0 flex-1 justify-center overflow-x-hidden overflow-y-auto px-4 pt-5 pb-6 [scrollbar-width:thin] md:px-6"
        >
          <div className={cn("flex w-full max-w-[620px] flex-col gap-4", messages.length ? "h-fit" : "h-full justify-center")}>
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

                {/* Summaries and notes the engine added to the conversation */}
                {turn.role === "system" && turn.content && (
                  <details className="glass-control rounded-md px-3 py-2 text-[12.5px] text-muted-foreground">
                    <summary className="cursor-pointer select-none outline-none hover:text-foreground">
                      {turn.kind === "compaction" ? "Earlier conversation was compacted" : turn.kind === "branch" ? "Branch summary" : "Note"}
                    </summary>
                    <div className="pt-2">
                      <MarkdownContent content={turn.content} />
                    </div>
                  </details>
                )}

                {/* Assistant Message */}
                {turn.role === "assistant" && (
                  <div className="flex flex-col gap-3 text-sm leading-relaxed">
                    {turn.thinking ? (
                      <ThinkingBlock
                        text={turn.thinking}
                        seconds={turn.thinkingDurationSec}
                        streaming={turn.isStreaming && !turn.content && !turn.toolCalls?.length}
                      />
                    ) : null}

                    {/* Tool Steps */}
                    {!!turn.toolCalls?.length && (
                      <StepGroup
                        steps={turn.toolCalls.map((tc: ToolExecution) => toolToStep(tc))}
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

            {todos.length > 0 && <AgentTodos todos={todos} />}

            {askPrompt && (
              <AskDialog
                key={askPrompt.id}
                prompt={askPrompt}
                onSubmit={(answers) => submitAnswers(askPrompt.id, answers)}
                onSkip={() => submitAnswers(askPrompt.id, null)}
              />
            )}

            {uiPrompt && (
              uiPrompt.kind === "select" ? (
                <AskCard
                  eyebrow="Agent question"
                  question={uiPrompt.title}
                  options={uiPrompt.options || []}
                  onAnswer={(opt) => submitUiInput(uiPrompt.id, opt)}
                />
              ) : (
                <form
                  className="glass-panel flex flex-col gap-2 rounded-md px-3.5 py-3"
                  onSubmit={(e) => {
                    e.preventDefault()
                    const v = new FormData(e.currentTarget).get("v")
                    submitUiInput(uiPrompt.id, String(v ?? ""))
                  }}
                >
                  <p className="text-[12.5px]">{uiPrompt.title}</p>
                  <textarea
                    name="v"
                    defaultValue={uiPrompt.prefill}
                    rows={4}
                    className="rounded-sm bg-background/60 p-2 text-[12.5px] outline-none"
                  />
                  <button type="submit" className="glass-chosen h-7 self-end rounded-sm px-3 text-[12.5px]">
                    Submit
                  </button>
                </form>
              )
            )}

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
              <div className="flex flex-col items-center gap-1.5 text-center">
                <p className="text-[15px] text-foreground/90">What should we work on?</p>
                <p className="text-xs text-muted-foreground">
                  Ask about your sites and servers, or type <kbd className="rounded-[3px] bg-foreground/10 px-1 font-mono">/</kbd> for skills and commands.
                </p>
              </div>
            )}
          </div>
        </div>

        {/* Composer Dock */}
        <div className="flex shrink-0 justify-center px-4 pb-4 md:px-6">
          <div className="w-full max-w-[620px]">
            {notice && (
              <button
                type="button"
                onClick={dismissNotice}
                className={cn(
                  "glass-panel mb-2 w-full rounded-md px-3 py-1.5 text-left text-[12px]",
                  notice.level === "error" && "text-destructive",
                  notice.level === "warning" && "text-status-interrupted"
                )}
              >
                {notice.text}
              </button>
            )}
            {/* Queued Message Banner */}
            {queuedMessage && (
              <div className="mb-2 flex items-center justify-between gap-2 rounded-xl border border-orange-500/30 bg-orange-500/10 px-3 py-2 text-xs shadow-lg backdrop-blur-md animate-in fade-in slide-in-from-bottom-2 duration-150">
                <div className="flex min-w-0 items-center gap-2">
                  <span className="flex items-center gap-1.5 rounded-full bg-orange-500/20 px-2 py-0.5 text-[10px] font-mono font-semibold uppercase tracking-wider text-orange-400 border border-orange-500/30">
                    <span className="size-1.5 rounded-full bg-orange-400 animate-pulse" />
                    Queued message
                  </span>
                  <span className="truncate text-zinc-200 font-sans max-w-[320px] sm:max-w-[420px]">
                    {queuedMessage.displaySnippet}
                  </span>
                </div>
                <div className="flex items-center gap-1.5 shrink-0">
                  <button
                    type="button"
                    title="Send now (interrupts current turn)"
                    onClick={handleSendQueuedNow}
                    className="inline-flex size-6 items-center justify-center rounded-md bg-orange-500 text-white hover:bg-orange-600 transition-colors shadow-xs"
                  >
                    <ArrowUp className="size-3.5" />
                  </button>
                  <button
                    type="button"
                    title="Cancel queued message"
                    onClick={handleCancelQueued}
                    className="inline-flex size-6 items-center justify-center rounded-md bg-white/[0.08] text-zinc-400 hover:bg-white/[0.16] hover:text-zinc-200 transition-colors"
                  >
                    <XIcon className="size-3.5" />
                  </button>
                </div>
              </div>
            )}

            <Composer
              textareaProps={{
                "aria-label": "Message",
                onPaste: (e: React.ClipboardEvent<HTMLTextAreaElement>) => {
                  const files = Array.from(e.clipboardData.files)
                  if (files.length) {
                    e.preventDefault()
                    void attachLocal(files)
                  }
                },
              } as any}
              value={value}
              onValueChange={setValue}
              onSubmit={handleSend}
              running={isProcessing}
              onStop={abort}
              slashCommands={flattenedSlashCommands}
              onBackspaceAtStart={() => {
                if (images.length) {
                  setImages((all) => all.slice(0, -1))
                  return true
                }
                if (!attached.length) return false
                setAttached((all) => all.slice(0, -1))
                return true
              }}
              placeholder="Ask NextDeploy AI, or type / for skills and commands…"
              canSubmit={value.trim().length > 0 || images.length > 0}
              leading={
                <>
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label="Attach files or images"
                    title="Attach files or images from this computer"
                    disabled={uploading}
                    onClick={() => attachInput.current?.click()}
                    className="shrink-0 text-muted-foreground"
                  >
                    <PlusIcon className={cn("size-4", uploading && "animate-pulse")} />
                  </Button>
                  <input
                    ref={attachInput}
                    type="file"
                    multiple
                    hidden
                    onChange={(e) => {
                      if (e.target.files?.length) void attachLocal(e.target.files)
                      e.target.value = ""
                    }}
                  />
                </>
              }
              above={
                attached.length || images.length ? (
                  <>
                    {attached.map((path) => (
                      <Tag
                        key={path}
                        className="mb-0.5"
                        icon={<FileTextIcon />}
                        onRemove={() => setAttached((all) => all.filter((each) => each !== path))}
                      >
                        <span className="block max-w-[10rem] truncate">{path.split("/").pop()}</span>
                      </Tag>
                    ))}
                    {images.map((img, i) => (
                      <Tag
                        key={`${img.name}-${i}`}
                        className="mb-0.5"
                        icon={<img alt="" src={`data:${img.mimeType};base64,${img.data}`} className="size-3.5 rounded-[3px] object-cover" />}
                        onRemove={() => setImages((all) => all.filter((_, j) => j !== i))}
                      >
                        <span className="block max-w-[10rem] truncate">{img.name}</span>
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
                value={status.autonomyMode || "write"}
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

                {effortOptions.length > 0 && (
                  <ComposerPick
                    label="Effort"
                    value={thinkingLevel || effortOptions[0]?.value || "medium"}
                    options={effortOptions}
                    shown={effortOptions.find((e) => e.value === thinkingLevel)?.label || effortOptions[0]?.label}
                    onValueChange={(eff) => setThinkingLevel(eff as any)}
                  />
                )}

                {contextUsage && contextUsage.contextWindow > 0 && (
                  <ContextMeter
                    used={contextUsage.tokens}
                    window={contextUsage.contextWindow}
                    stats={stats}
                    onCompact={() => compactContext()}
                    disabled={isProcessing}
                  />
                )}
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
                    label="Refresh"
                    className="size-6 [&_svg]:size-3.5"
                    onClick={() => loadWorkspaceFiles()}
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
                    if (!attached.includes(path)) setAttached((all) => [...all, path])
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
                {runningTasks.length === 0 ? (
                  <div className="px-3 py-3 text-center text-[11.5px] text-muted-foreground/60 border border-dashed border-border/40 rounded-md mx-1.5 mb-1.5">
                    No active background tasks or subagents
                  </div>
                ) : (
                  <ul className="flex flex-col gap-1.5 px-1.5">
                    {runningTasks.map((task, idx) => (
                      <TaskBoard key={task.id || idx} task={task} onStop={() => cancelSubagent(task.id)} />
                    ))}
                  </ul>
                )}

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
                      <TaskBoard key={task.id || idx} task={task} onStop={() => {}} />
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
          onRemoveProvider={removeProvider}
        />
      )}
    </div>
  )
}

export default HextaAICanvas
