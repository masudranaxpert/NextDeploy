import * as React from "react"
import { ShrinkIcon } from "lucide-react"

import { cn } from "@/lib/utils"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import type { SessionStats } from "./types"

const compact = (n: number) =>
  n >= 1_000_000 ? `${+(n / 1_000_000).toFixed(1)}M` : n >= 1_000 ? `${+(n / 1_000).toFixed(1)}k` : String(n)

/** Context window fill as a small ring; opened, the numbers omp reports for the session. */
export function ContextMeter({
  used,
  window: size,
  stats,
  onCompact,
  disabled,
}: {
  used: number
  window: number
  stats: SessionStats | null
  onCompact: () => void
  disabled?: boolean
}) {
  const fraction = size > 0 ? Math.min(used / size, 1) : 0
  const percent = fraction * 100
  const tone = fraction >= 0.9 ? "bg-destructive" : fraction >= 0.7 ? "bg-amber-500" : "bg-sky-500"
  const ringTone = fraction >= 0.9 ? "text-destructive" : fraction >= 0.7 ? "text-amber-500" : "text-sky-500"
  const r = 6
  const around = 2 * Math.PI * r

  const rows: Array<[string, string]> = stats
    ? [
        ["Messages", String(stats.userMessages + stats.assistantMessages)],
        ["Tool calls", String(stats.toolCalls)],
        ["Input tokens", stats.tokens.input.toLocaleString()],
        ["Output tokens", stats.tokens.output.toLocaleString()],
        ...(stats.tokens.cacheRead ? ([["Cached tokens", stats.tokens.cacheRead.toLocaleString()]] as Array<[string, string]>) : []),
        ...(stats.cost ? ([["Cost", `$${stats.cost.toFixed(4)}`]] as Array<[string, string]>) : []),
      ]
    : []

  return (
    <Popover>
      <PopoverTrigger
        aria-label={`Context window ${Math.round(percent)}% used`}
        title="Context window"
        className="flex size-8 items-center justify-center rounded-sm outline-none hover:bg-accent focus-visible:bg-accent md:size-6"
      >
        <svg viewBox="0 0 16 16" className={cn("size-4", ringTone)} aria-hidden>
          <circle cx="8" cy="8" r={r} fill="none" stroke="currentColor" strokeOpacity="0.25" strokeWidth="2" />
          <circle
            cx="8"
            cy="8"
            r={r}
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeDasharray={`${around * fraction} ${around}`}
            transform="rotate(-90 8 8)"
          />
        </svg>
      </PopoverTrigger>
      <PopoverContent align="end" side="top" className="w-72 max-w-[calc(100vw-2rem)] p-3">
        <div className="flex items-baseline justify-between gap-3">
          <span className="text-[13px] font-medium">Context window</span>
          <span className="text-[12.5px] text-muted-foreground tabular-nums">
            {compact(used)} / {compact(size)} · {percent < 1 && used > 0 ? "<1" : Math.round(percent)}%
          </span>
        </div>
        <div className="mt-2 h-1.5 overflow-hidden rounded-full bg-foreground/10" role="progressbar" aria-valuenow={Math.round(percent)} aria-valuemin={0} aria-valuemax={100}>
          <div className={cn("h-full rounded-full transition-[width]", tone)} style={{ width: `${Math.max(percent, used > 0 ? 1.5 : 0)}%` }} />
        </div>
        {rows.length > 0 && (
          <dl className="mt-3 grid grid-cols-[1fr_auto] gap-x-4 gap-y-1.5 text-[12.5px]">
            {rows.map(([k, v]) => (
              <React.Fragment key={k}>
                <dt className="text-muted-foreground">{k}</dt>
                <dd className="text-right tabular-nums">{v}</dd>
              </React.Fragment>
            ))}
          </dl>
        )}
        <button
          type="button"
          disabled={disabled}
          onClick={onCompact}
          className="mt-3 inline-flex h-9 w-full items-center justify-center gap-1.5 rounded-[4px] border border-border/60 text-[13px] transition-colors outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring/60 disabled:pointer-events-none disabled:opacity-50"
        >
          <ShrinkIcon className="size-3.5" /> Compact
        </button>
      </PopoverContent>
    </Popover>
  )
}
