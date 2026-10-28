import * as React from "react"
import { Loader2, X } from "lucide-react"

import { cn } from "@/lib/utils"

/**
 * Something attached to what is being written: a tool, a file, a folder. A
 * small pane of clear glass, sized to its label and never to a floor — a
 * minimum width leaves short names with dead space before the close button,
 * so the x sits at a different distance on every tag.
 *
 * While `progress` is between 0 and 1 the tag itself is the progress bar: it
 * fills from the left, so nothing new appears in the row. `armed` is the same
 * pane lit a step brighter, for the tag the next Backspace will remove.
 */
function Tag({
  icon,
  progress,
  error,
  armed,
  onRemove,
  removeLabel = "Remove",
  className,
  children,
  ...props
}: React.ComponentProps<"span"> & {
  icon?: React.ReactNode
  /** 0–1 while something is on its way; left out once it has arrived. */
  progress?: number
  /** What went wrong, said on hover; the tag turns to the colour of failure. */
  error?: string
  armed?: boolean
  onRemove?: () => void
  removeLabel?: string
}) {
  const going = progress !== undefined && !error
  const percent = Math.round((progress ?? 0) * 100)
  return (
    <span
      data-slot="tag"
      title={error}
      className={cn(
        "glass-chosen relative flex h-7 w-fit max-w-44 items-center gap-1.5 overflow-hidden rounded-sm pl-2 text-[12px] text-foreground/90 [&_svg]:size-3.5 [&_svg]:shrink-0",
        onRemove ? "pr-1" : "pr-2",
        error && "text-destructive",
        armed && "glass-armed",
        className
      )}
      {...props}
    >
      {going && (
        <span
          aria-hidden="true"
          className="absolute inset-y-0 left-0 bg-primary/15 transition-[width] duration-150"
          style={{ width: `${percent}%` }}
        />
      )}
      <span className={cn("relative flex", error ? "text-destructive" : "text-muted-foreground")}>
        {error ? <X /> : going ? <Loader2 className="animate-spin" /> : icon}
      </span>
      <span className="relative truncate">{children}</span>
      {/* As wide as "100%" from the start, so the tag does not grow, and push its neighbours along, as the count gains a digit. */}
      {going && <span className="relative min-w-[4ch] shrink-0 text-right text-[10px] text-muted-foreground tabular-nums">{percent}%</span>}
      {onRemove && (
        <button
          type="button"
          aria-label={removeLabel}
          onClick={onRemove}
          className="relative shrink-0 rounded p-0.5 text-muted-foreground outline-none hover:text-foreground focus-visible:text-foreground [&_svg]:size-3"
        >
          <X />
        </button>
      )}
    </span>
  )
}

export { Tag }
