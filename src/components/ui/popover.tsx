import * as React from "react"
import { createPortal } from "react-dom"
import { cn } from "@/lib/utils"

interface PopoverContextType {
  open: boolean
  setOpen: React.Dispatch<React.SetStateAction<boolean>>
  triggerRef: React.RefObject<HTMLElement | null>
}

const PopoverContext = React.createContext<PopoverContextType | null>(null)

function Popover({
  children,
  open: controlledOpen,
  onOpenChange,
}: {
  children: React.ReactNode
  open?: boolean
  onOpenChange?: (open: boolean) => void
}) {
  const [uncontrolledOpen, setUncontrolledOpen] = React.useState(false)
  const isControlled = controlledOpen !== undefined
  const open = isControlled ? controlledOpen : uncontrolledOpen
  const triggerRef = React.useRef<HTMLElement | null>(null)

  const setOpen = React.useCallback(
    (value: React.SetStateAction<boolean>) => {
      const next = typeof value === "function" ? value(open) : value
      if (!isControlled) setUncontrolledOpen(next)
      onOpenChange?.(next)
    },
    [isControlled, open, onOpenChange]
  )

  return (
    <PopoverContext.Provider value={{ open, setOpen, triggerRef }}>
      {children}
    </PopoverContext.Provider>
  )
}

function PopoverTrigger({
  asChild,
  children,
  className,
  onClick,
  ...props
}: React.ComponentProps<"button"> & { asChild?: boolean }) {
  const ctx = React.useContext(PopoverContext)
  if (!ctx) throw new Error("PopoverTrigger must be inside Popover")

  const handleClick = (e: React.MouseEvent<HTMLButtonElement>) => {
    ctx.setOpen((prev) => !prev)
    onClick?.(e)
  }

  return (
    <button
      ref={ctx.triggerRef as React.RefObject<HTMLButtonElement>}
      type="button"
      data-slot="popover-trigger"
      aria-expanded={ctx.open}
      onClick={handleClick}
      className={className}
      {...props}
    >
      {children}
    </button>
  )
}

function PopoverContent({
  children,
  className,
  side = "bottom",
  align = "center",
  sideOffset = 6,
  ...props
}: React.ComponentProps<"div"> & {
  side?: "top" | "bottom" | "left" | "right"
  align?: "start" | "center" | "end"
  sideOffset?: number
}) {
  const ctx = React.useContext(PopoverContext)
  if (!ctx) throw new Error("PopoverContent must be inside Popover")

  const [coords, setCoords] = React.useState<{ top: number; left: number }>({ top: 0, left: 0 })
  const contentRef = React.useRef<HTMLDivElement>(null)

  React.useLayoutEffect(() => {
    if (!ctx.open || !ctx.triggerRef.current) return

    const updatePosition = () => {
      const triggerEl = ctx.triggerRef.current
      if (!triggerEl) return
      const rect = triggerEl.getBoundingClientRect()
      const contentEl = contentRef.current
      const contentWidth = contentEl ? contentEl.offsetWidth : 288
      const contentHeight = contentEl ? contentEl.offsetHeight : 150

      let top = 0
      let left = 0

      if (side === "top") {
        top = rect.top - contentHeight - sideOffset
      } else {
        top = rect.bottom + sideOffset
      }

      if (align === "start") {
        left = rect.left
      } else if (align === "end") {
        left = rect.right - contentWidth
      } else {
        left = rect.left + rect.width / 2 - contentWidth / 2
      }

      // Constrain within viewport
      const padding = 8
      left = Math.max(padding, Math.min(left, window.innerWidth - contentWidth - padding))
      top = Math.max(padding, Math.min(top, window.innerHeight - contentHeight - padding))

      setCoords({ top, left })
    }

    updatePosition()
    window.addEventListener("resize", updatePosition)
    window.addEventListener("scroll", updatePosition, true)
    return () => {
      window.removeEventListener("resize", updatePosition)
      window.removeEventListener("scroll", updatePosition, true)
    }
  }, [ctx.open, side, align, sideOffset])

  React.useEffect(() => {
    if (!ctx.open) return

    const handlePointerDown = (e: MouseEvent) => {
      const target = e.target as Node
      if (
        contentRef.current?.contains(target) ||
        ctx.triggerRef.current?.contains(target)
      ) {
        return
      }
      ctx.setOpen(false)
    }

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") ctx.setOpen(false)
    }

    document.addEventListener("mousedown", handlePointerDown)
    document.addEventListener("keydown", handleKeyDown)
    return () => {
      document.removeEventListener("mousedown", handlePointerDown)
      document.removeEventListener("keydown", handleKeyDown)
    }
  }, [ctx])

  if (!ctx.open) return null

  return createPortal(
    <div
      ref={contentRef}
      data-slot="popover-content"
      style={{
        position: "fixed",
        top: `${coords.top}px`,
        left: `${coords.left}px`,
      }}
      className={cn(
        "z-50 flex w-72 flex-col gap-2.5 rounded-lg bg-popover p-2.5 text-sm text-popover-foreground shadow-md ring-1 ring-foreground/10 duration-100 animate-in fade-in-0 zoom-in-95",
        className
      )}
      {...props}
    >
      {children}
    </div>,
    document.body
  )
}

export { Popover, PopoverTrigger, PopoverContent }
