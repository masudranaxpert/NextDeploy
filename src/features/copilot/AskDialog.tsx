import * as React from "react"

import { cn } from "@/lib/utils"
import type { AskPrompt } from "./types"

type Answer = { id: string; selectedOptions: string[]; customInput?: string }

/** omp's `ask` tool: one or more questions, each single/multi choice with an optional free-text answer. */
export function AskDialog({
  prompt,
  onSubmit,
  onSkip,
}: {
  prompt: AskPrompt
  onSubmit: (answers: Answer[]) => void
  onSkip: () => void
}) {
  const [picked, setPicked] = React.useState<Record<string, string[]>>(() =>
    Object.fromEntries(
      prompt.questions.map((q) => [
        q.id,
        q.recommended !== undefined && q.options[q.recommended] ? [q.options[q.recommended].label] : [],
      ])
    )
  )
  const [custom, setCustom] = React.useState<Record<string, string>>({})

  const toggle = (qid: string, label: string, multi: boolean) =>
    setPicked((all) => {
      const cur = all[qid] || []
      if (!multi) return { ...all, [qid]: [label] }
      return { ...all, [qid]: cur.includes(label) ? cur.filter((l) => l !== label) : [...cur, label] }
    })

  const ready = prompt.questions.every((q) => (picked[q.id]?.length || 0) > 0 || (custom[q.id] || "").trim())

  return (
    <div data-slot="ask-dialog" className="glass-panel flex flex-col gap-4 rounded-md px-3.5 py-3">
      <p className="text-[11.5px] text-status-interrupted">Needs your decision</p>
      {prompt.questions.map((q) => (
        <fieldset key={q.id} className="flex flex-col gap-2">
          {q.header && <legend className="text-[11px] tracking-wide text-muted-foreground uppercase">{q.header}</legend>}
          <p className="text-[14px] leading-relaxed whitespace-pre-wrap">{q.question}</p>
          <div className="flex flex-col gap-1.5">
            {q.options.map((o, i) => {
              const on = picked[q.id]?.includes(o.label)
              return (
                <button
                  key={o.label}
                  type="button"
                  aria-pressed={on}
                  onClick={() => toggle(q.id, o.label, q.multi)}
                  className={cn(
                    "flex flex-col items-start rounded-sm border px-3 py-1.5 text-left text-[13px] transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring/60",
                    on ? "glass-chosen border-transparent" : "border-border/60 hover:bg-accent"
                  )}
                >
                  <span>
                    {o.label}
                    {q.recommended === i && <span className="ml-2 text-[11px] text-muted-foreground">recommended</span>}
                  </span>
                  {o.description && <span className="text-[12px] text-muted-foreground">{o.description}</span>}
                </button>
              )
            })}
            <input
              value={custom[q.id] || ""}
              onChange={(e) => setCustom((all) => ({ ...all, [q.id]: e.target.value }))}
              placeholder="Or type your own answer"
              className="h-8 rounded-sm bg-background/60 px-2.5 text-[13px] outline-none placeholder:text-muted-foreground"
            />
          </div>
        </fieldset>
      ))}
      <div className="flex justify-end gap-2">
        <button type="button" onClick={onSkip} className="h-7 rounded-sm px-3 text-[12.5px] text-muted-foreground hover:bg-accent">
          Skip
        </button>
        <button
          type="button"
          disabled={!ready}
          onClick={() =>
            onSubmit(
              prompt.questions.map((q) => ({
                id: q.id,
                selectedOptions: picked[q.id] || [],
                customInput: (custom[q.id] || "").trim() || undefined,
              }))
            )
          }
          className="glass-chosen h-7 rounded-sm px-3 text-[12.5px] disabled:pointer-events-none disabled:opacity-50"
        >
          Send answer
        </button>
      </div>
    </div>
  )
}
