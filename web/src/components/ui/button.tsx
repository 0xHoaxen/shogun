import { Button as ButtonPrimitive } from "@base-ui/react/button"
import { cva, type VariantProps } from "class-variance-authority"

import { cn } from "@/lib/utils"

// Square, ruled buttons: hover inverts, primary hover turns signal, a disabled
// control is dashed. One primary per cell.
const buttonVariants = cva(
  "inline-flex min-h-9 shrink-0 cursor-pointer items-center justify-center gap-2 border border-line2 px-3.5 text-[11px] font-bold uppercase tracking-[0.06em] whitespace-nowrap transition-colors select-none disabled:cursor-not-allowed disabled:border-dashed disabled:bg-transparent disabled:text-muted-foreground disabled:hover:bg-transparent disabled:hover:text-muted-foreground",
  {
    variants: {
      variant: {
        default: "bg-transparent text-text hover:bg-cream hover:text-on-accent",
        primary:
          "bg-cream text-on-accent hover:border-scheme hover:bg-scheme hover:text-on-accent",
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
