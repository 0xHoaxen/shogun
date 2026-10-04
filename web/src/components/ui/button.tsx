import { Button as ButtonPrimitive } from "@base-ui/react/button"
import { cva, type VariantProps } from "class-variance-authority"

import { cn } from "@/lib/utils"

// Square, ruled buttons: hover inverts, primary hover turns signal, a disabled
// control is dashed. One primary per cell.
const buttonVariants = cva(
  "inline-flex min-h-9 shrink-0 cursor-pointer items-center justify-center gap-2 border border-ink px-3.5 text-[11px] font-bold uppercase tracking-[0.06em] whitespace-nowrap transition-colors select-none disabled:cursor-not-allowed disabled:border-dashed disabled:bg-transparent disabled:text-[#767676] disabled:hover:bg-transparent disabled:hover:text-[#767676]",
  {
    variants: {
      variant: {
        default: "bg-transparent text-ink hover:bg-ink hover:text-paper",
        primary:
          "bg-ink text-paper hover:border-signal hover:bg-signal hover:text-ink",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
)

function Button({
  className,
  variant = "default",
  ...props
}: ButtonPrimitive.Props & VariantProps<typeof buttonVariants>) {
  return (
    <ButtonPrimitive
      data-slot="button"
      className={cn(buttonVariants({ variant }), className)}
      {...props}
    />
  )
}

export { Button, buttonVariants }
