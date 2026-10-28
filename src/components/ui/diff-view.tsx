import { cn } from "@/lib/utils"

/** File headers and changed text can have the same prefix, even "+++ ". */
function patchLines(patch: string): string[] {
  const lines: string[] = []
  let inHunk = false
  let oldLines = 0
  let newLines = 0
  for (const line of patch.replace(/\n$/, "").split("\n")) {
    if (line.startsWith("diff --git ")) inHunk = false
    const hunk = /^@@ -\d+(?:,(\d+))? \+\d+(?:,(\d+))? @@/.exec(line)
    if (hunk) {
      oldLines = Number(hunk[1] ?? 1)
      newLines = Number(hunk[2] ?? 1)
      inHunk = oldLines > 0 || newLines > 0
    } else if (line.startsWith("@@")) {
      // Also accept abbreviated hunks, as used in compact tool output.
      inHunk = true
      oldLines = newLines = Infinity
    } else if (!inHunk && /^(---|\+\+\+) /.test(line)) {
      continue
    } else if (inHunk) {
      if (line.startsWith("-") || line.startsWith(" ")) oldLines--
      if (line.startsWith("+") || line.startsWith(" ")) newLines--
      if (oldLines <= 0 && newLines <= 0) inHunk = false
    }
    lines.push(line)
  }
  return lines
}

/** Lines added and removed by a unified patch, headers aside. */
function patchStats(patch: string): { added: number; removed: number } {
  let added = 0
  let removed = 0
  for (const line of patchLines(patch)) {
    if (line.startsWith("+")) added++
    else if (line.startsWith("-")) removed++
  }
  return { added, removed }
}

function DiffStats({ added, removed, className }: { added: number; removed: number; className?: string }) {
  return (
    <span data-slot="diff-stats" className={cn("shrink-0 text-[12px] tabular-nums", className)}>
      <span className="text-emerald-600 dark:text-emerald-400">+{added}</span>{" "}
      <span className="text-rose-600 dark:text-rose-400">−{removed}</span>
    </span>
  )
}

/** A patch as a diff: hunks with added and removed lines coloured, headers dropped. */
function DiffView({ patch, className }: { patch: string; className?: string }) {
  const lines = patchLines(patch)
  return (
    <div
      data-slot="diff-view"
      className={cn("glass-panel scrollbar-thin max-h-80 overflow-auto rounded-md py-1 font-mono text-[11.5px] leading-[1.6]", className)}
    >
      {lines.map((line, index) => (
        <div
          key={index}
          className={cn(
            "min-w-fit px-3 whitespace-pre",
            line.startsWith("+") && "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
            line.startsWith("-") && "bg-rose-500/10 text-rose-700 dark:text-rose-300",
            line.startsWith("@@") && "mt-1 text-muted-foreground/70 first:mt-0",
            line.startsWith("...") && "text-muted-foreground/70 italic"
          )}
        >
          {line || " "}
        </div>
      ))}
    </div>
  )
}

export { DiffView, DiffStats, patchStats }
