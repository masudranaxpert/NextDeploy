import * as React from "react"
import { X } from "lucide-react"

import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"

/**
 * The thinnest pane: one of the panels a page is laid out in — a name,
 * whatever the panel wants to offer beside it, a way to close it, and its
 * content. A shade off the page rather than a card's brighter pane, so a
 * column of them does not outshine what is being read.
 *
 * A panel whose own controls already say what it is — a row of tabs — passes
 * them as `bar` and they take the name's place.
 */
function Panel({
  title,
  bar,
  actions,
  onClose,
  closeLabel = "Close",
  className,
  children,
  ...props
}: Omit<React.ComponentProps<"section">, "title"> & {
  title: string
  bar?: React.ReactNode
  actions?: React.ReactNode
  /** Without it the panel has no close button. */
  onClose?: () => void
  closeLabel?: string
}) {
  return (
    <section
      data-slot="panel"
      aria-label={title}
      className={cn("glass-panel flex min-h-0 flex-col overflow-hidden rounded-md", className)}
      {...props}
    >
      <header data-slot="panel-header" className={cn("flex h-8 shrink-0 items-center gap-0.5 pr-1", bar ? "pl-1" : "pl-2.5")}>
        {bar ?? <h2 className="mr-auto truncate text-[12.5px] font-medium text-foreground/90">{title}</h2>}
        {actions}
        {onClose && (
          <Button
            variant="ghost"
            size="icon-xs"
            aria-label={`${closeLabel} ${title}`}
            onClick={onClose}
            className="text-muted-foreground hover:text-foreground"
          >
            <X className="size-3.5" />
          </Button>
        )}
      </header>
      {children}
    </section>
  )
}

/** A row in a panel's list: lit while under the pointer, a pane of clear glass when chosen. */
function PanelRow({
  chosen,
  className,
  ...props
}: React.ComponentProps<"button"> & { chosen?: boolean }) {
  return (
    <button
      type="button"
      data-slot="panel-row"
      aria-current={chosen || undefined}
      className={cn(
        "flex h-7 w-full items-center gap-2 rounded-sm px-2 text-left text-[12.5px] text-foreground/85 transition-colors outline-none hover:bg-accent focus-visible:bg-accent [&_svg]:size-3.5 [&_svg]:shrink-0 [&_svg]:text-muted-foreground",
        chosen && "glass-chosen text-foreground",
        className
      )}
      {...props}
    />
  )
}

export { Panel, PanelRow }
