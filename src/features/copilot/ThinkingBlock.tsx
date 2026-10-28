import * as React from "react"
import { ChevronRightIcon } from "lucide-react"

import { cn } from "@/lib/utils"
import { MarkdownContent } from "./MarkdownContent"

/** The model's reasoning, folded under "Thought for Ns" once it has finished. */
export function ThinkingBlock({
  text,
  seconds,
  streaming,
}: {
  text: string
  seconds?: number
  streaming?: boolean
}) {
  const [open, setOpen] = React.useState(false)
  const label = streaming ? "Thinking" : seconds ? `Thought for ${seconds}s` : "Thought"
  return (
    <div data-slot="thinking" className="flex flex-col gap-1.5">
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className="flex w-fit items-center gap-2 text-[13px] text-muted-foreground transition-colors outline-none hover:text-foreground focus-visible:text-foreground"
      >
        <span
          aria-hidden
          className={cn("size-3.5 shrink-0 rounded-full bg-gradient-to-br from-sky-400 to-indigo-500", streaming && "animate-pulse")}
        />
        <span className={cn(streaming && "status-shimmer")}>{label}</span>
        <ChevronRightIcon className={cn("size-3.5 transition-transform", open && "rotate-90")} />
      </button>
      {open && (
        <div className="border-l border-border/60 pl-3 text-[13px] text-muted-foreground">
          <MarkdownContent content={text} isStreaming={streaming} />
        </div>
      )}
    </div>
  )
}
