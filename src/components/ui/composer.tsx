import * as React from "react"
import { ArrowUp, ChevronDown, Check, Square } from "lucide-react"

import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"

export interface SlashCommandItem {
  name: string
  description?: string
  hint?: string
}

function Composer({
  value,
  onValueChange,
  onSubmit,
  onStop,
  running = false,
  placeholder = "Keep going, or change direction…",
  textareaProps,
  leading,
  tags,
  above,
  canSubmit,
  onBackspaceAtStart,
  sendLabel = "Send",
  stopLabel = "Stop",
  slashCommands = [],
  className,
  ...props
}: Omit<React.ComponentProps<"div">, "onSubmit"> & {
  value: string
  onValueChange: (value: string) => void
  onSubmit?: (value: string) => void
  onStop?: () => void
  running?: boolean
  placeholder?: string
  textareaProps?: Pick<React.ComponentProps<"textarea">, "id" | "aria-label" | "aria-labelledby" | "aria-describedby">
  leading?: React.ReactNode
  tags?: React.ReactNode
  above?: React.ReactNode
  canSubmit?: boolean
  onBackspaceAtStart?: () => boolean
  sendLabel?: string
  stopLabel?: string
  slashCommands?: SlashCommandItem[]
}) {
  const textarea = React.useRef<HTMLTextAreaElement>(null)
  const ready = canSubmit ?? value.trim().length > 0
  const stops = running && !ready
  const Icon = stops ? Square : ArrowUp

  // Slash commands autocomplete state
  const [slashIndex, setSlashIndex] = React.useState(0)
  const [slashOpen, setSlashOpen] = React.useState(false)

  // Detect slash command at start or active word
  const matchSlash = value.startsWith("/") ? value.slice(1).trim().toLowerCase() : null
  const filteredCommands = React.useMemo(() => {
    if (matchSlash === null) return []
    return slashCommands.filter(
      (cmd) =>
        cmd.name.toLowerCase().includes(matchSlash) ||
        (cmd.description && cmd.description.toLowerCase().includes(matchSlash))
    )
  }, [matchSlash, slashCommands])

  React.useEffect(() => {
    if (matchSlash !== null && filteredCommands.length > 0) {
      setSlashOpen(true)
      setSlashIndex(0)
    } else {
      setSlashOpen(false)
    }
  }, [matchSlash, filteredCommands.length])

  const resize = React.useCallback(() => {
    const el = textarea.current
    if (!el) return
    el.style.height = "auto"
    el.style.height = `${el.scrollHeight}px`
  }, [])

  React.useLayoutEffect(resize, [value, resize])

  React.useLayoutEffect(() => {
    const el = textarea.current
    if (!el || typeof ResizeObserver === "undefined") return
    let width = -1
    const observer = new ResizeObserver(([entry]) => {
      if (entry.contentRect.width === width) return
      width = entry.contentRect.width
      resize()
    })
    observer.observe(el)
    return () => observer.disconnect()
  }, [resize])

  const submit = () => {
    if (!ready) return
    setSlashOpen(false)
    onSubmit?.(value)
  }

  const applySlashCommand = (cmd: SlashCommandItem) => {
    onValueChange(`/${cmd.name} `)
    setSlashOpen(false)
    textarea.current?.focus()
  }

  return (
    <div
      data-slot="composer"
      className={cn(
        "group/composer relative rounded-xl bg-card py-2 pr-1 pl-2 transition-colors focus-within:border-ring/60",
        className
      )}
      {...props}
    >
      {/* Slash command popover floating above composer */}
      {slashOpen && filteredCommands.length > 0 && (
        <div
          data-slot="slash-menu"
          className="glass-panel absolute bottom-full left-0 mb-2 w-72 overflow-hidden rounded-lg p-1.5 shadow-xl ring-1 ring-foreground/10 z-50 animate-in fade-in-0 zoom-in-95"
        >
          <div className="px-2 py-1 text-[11px] font-medium text-muted-foreground">Slash Commands</div>
          <div className="max-h-48 overflow-y-auto [scrollbar-width:thin] flex flex-col gap-0.5">
            {filteredCommands.map((cmd, i) => (
              <button
                key={cmd.name}
                type="button"
                onClick={() => applySlashCommand(cmd)}
                className={cn(
                  "flex w-full items-start gap-2 rounded px-2 py-1.5 text-left text-xs transition-colors outline-none",
                  i === slashIndex ? "glass-chosen text-foreground font-medium" : "text-foreground/80 hover:bg-accent"
                )}
              >
                <span className="font-mono text-accent-foreground font-semibold shrink-0">/{cmd.name}</span>
                <span className="flex-1 truncate text-muted-foreground">{cmd.description || cmd.hint || ""}</span>
              </button>
            ))}
          </div>
        </div>
      )}

      {above && <div className="flex flex-wrap gap-1.5 px-1 pt-0.5 pb-2">{above}</div>}

      <div className="flex flex-wrap items-end gap-1">
        {leading}
        {tags}
        <div className={cn("flex min-w-48 flex-1 items-end gap-1", Boolean(tags) && "ml-1.5", !leading && !tags && "pl-1.5")}>
          <textarea
            {...textareaProps}
            ref={textarea}
            rows={1}
            value={value}
            placeholder={placeholder}
            onChange={(event) => onValueChange(event.target.value)}
            onKeyDown={(event) => {
              if (event.nativeEvent.isComposing || event.keyCode === 229) return

              if (slashOpen && filteredCommands.length > 0) {
                if (event.key === "ArrowDown") {
                  event.preventDefault()
                  setSlashIndex((prev) => (prev + 1) % filteredCommands.length)
                  return
                }
                if (event.key === "ArrowUp") {
                  event.preventDefault()
                  setSlashIndex((prev) => (prev - 1 + filteredCommands.length) % filteredCommands.length)
                  return
                }
                if (event.key === "Enter" || event.key === "Tab") {
                  event.preventDefault()
                  applySlashCommand(filteredCommands[slashIndex])
                  return
                }
                if (event.key === "Escape") {
                  event.preventDefault()
                  setSlashOpen(false)
                  return
                }
              }

              if (
                event.key === "Backspace" &&
                event.currentTarget.selectionStart === 0 &&
                event.currentTarget.selectionEnd === 0 &&
                onBackspaceAtStart?.()
              ) {
                event.preventDefault()
                return
              }

              if (event.key === "Enter" && !event.shiftKey) {
                event.preventDefault()
                submit()
              }
            }}
            className={cn(
              "scrollbar-thin max-h-50 min-w-0 flex-1 resize-none bg-transparent py-1 text-sm leading-6 text-foreground outline-none placeholder:overflow-hidden placeholder:text-ellipsis placeholder:whitespace-nowrap placeholder:text-muted-foreground"
            )}
          />
          <Button
            size="icon"
            aria-label={stops ? stopLabel : sendLabel}
            disabled={!stops && !ready}
            onClick={stops ? onStop : submit}
            className={cn(
              "shrink-0 rounded-md bg-transparent transition-colors hover:bg-accent",
              stops
                ? "text-muted-foreground hover:text-foreground"
                : ready
                  ? "text-foreground"
                  : "text-muted-foreground/50 hover:bg-transparent disabled:opacity-100"
            )}
          >
            <Icon className={cn("size-4", stops && "size-2.5 fill-current")} />
          </Button>
        </div>
      </div>
    </div>
  )
}

function ComposerFooter({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="composer-footer"
      className={cn("mt-1 flex items-center gap-1 pr-[9px] pl-2.5 [&>[data-slot=composer-footer-end]]:ml-auto", className)}
      {...props}
    />
  )
}

function ComposerFooterEnd({ className, ...props }: React.ComponentProps<"div">) {
  return <div data-slot="composer-footer-end" className={cn("flex items-center gap-0.5", className)} {...props} />
}

function ComposerPick<T extends string>({
  label,
  value,
  options,
  onValueChange,
  shown,
  align = "end",
}: {
  label: string
  value: T
  options: { value: T; label: string; hint?: string }[]
  onValueChange: (value: T) => void
  shown?: string
  align?: "start" | "end"
}) {
  const [open, setOpen] = React.useState(false)
  const menuRef = React.useRef<HTMLDivElement>(null)
  const triggerRef = React.useRef<HTMLButtonElement>(null)
  const explained = options.some((option) => option.hint)

  React.useEffect(() => {
    if (!open) return
    const handleClickOutside = (e: MouseEvent) => {
      if (
        menuRef.current?.contains(e.target as Node) ||
        triggerRef.current?.contains(e.target as Node)
      ) {
        return
      }
      setOpen(false)
    }
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false)
    }
    document.addEventListener("mousedown", handleClickOutside)
    document.addEventListener("keydown", handleKeyDown)
    return () => {
      document.removeEventListener("mousedown", handleClickOutside)
      document.removeEventListener("keydown", handleKeyDown)
    }
  }, [open])

  const selectedOption = options.find((o) => o.value === value)

  return (
    <div className="relative inline-block">
      <button
        ref={triggerRef}
        type="button"
        aria-label={label}
        aria-expanded={open}
        onClick={() => setOpen((prev) => !prev)}
        className={cn(
          "glass-held flex h-6 items-center gap-1 rounded-sm px-1.5 text-[12px] text-muted-foreground transition-colors outline-none hover:text-foreground focus-visible:text-foreground",
          open && "text-foreground"
        )}
      >
        <span className="max-w-40 truncate">{shown ?? selectedOption?.label ?? value}</span>
        <ChevronDown className="size-3 opacity-60" />
      </button>

      {open && (
        <div
          ref={menuRef}
          data-slot="composer-pick-menu"
          className={cn(
            "glass-panel absolute bottom-full mb-1.5 z-50 rounded-lg p-1 text-popover-foreground shadow-lg ring-1 ring-foreground/10 outline-none animate-in fade-in-0 zoom-in-95",
            align === "start" ? "left-0" : "right-0",
            explained ? "w-72" : "min-w-36"
          )}
        >
          <div className="px-2 py-1 text-[11px] font-medium text-muted-foreground border-b border-border/50 mb-0.5">
            {label}
          </div>
          <div className="flex flex-col gap-0.5 max-h-56 overflow-y-auto [scrollbar-width:thin]">
            {options.map((option) => {
              const isSelected = option.value === value
              return (
                <button
                  key={option.value}
                  type="button"
                  onClick={() => {
                    onValueChange(option.value)
                    setOpen(false)
                  }}
                  className={cn(
                    "flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-[12.5px] transition-colors outline-none hover:bg-accent focus-visible:bg-accent",
                    isSelected ? "glass-chosen text-foreground font-medium" : "text-foreground/85"
                  )}
                >
                  <span className="flex size-3.5 shrink-0 items-center justify-center">
                    {isSelected && <Check className="size-3 text-primary" />}
                  </span>
                  {option.hint ? (
                    <span className="flex min-w-0 flex-col gap-0.5">
                      <span>{option.label}</span>
                      <span className="text-[11px] leading-snug whitespace-normal text-muted-foreground">
                        {option.hint}
                      </span>
                    </span>
                  ) : (
                    <span className="truncate">{option.label}</span>
                  )}
                </button>
              )
            })}
          </div>
        </div>
      )}
    </div>
  )
}

export { Composer, ComposerFooter, ComposerFooterEnd, ComposerPick }
