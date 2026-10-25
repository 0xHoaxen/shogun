import type { ReactNode } from "react";

import { cn } from "@/lib/utils";

interface ToggleProps {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  // The visible name; it also names the switch.
  label: string;
  // A line under the label that says what the switch changes.
  hint?: ReactNode;
  disabled?: boolean;
}

// An on/off switch (role=switch) with its label beside it.
export function Toggle({ checked, onCheckedChange, label, hint, disabled }: ToggleProps) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      disabled={disabled}
      onClick={() => onCheckedChange(!checked)}
      className="flex w-full cursor-pointer items-start gap-3 text-left disabled:cursor-not-allowed disabled:opacity-60"
    >
      <span
        aria-hidden="true"
        className={cn(
          "relative mt-0.5 box-border h-[18px] w-[34px] shrink-0 border border-line2 transition-colors after:absolute after:top-[3px] after:left-[3px] after:size-2.5 after:bg-muted-foreground after:transition-transform after:content-['']",
          checked && "border-scheme bg-scheme after:translate-x-4 after:bg-on-accent",
        )}
      />
      <span className="flex min-w-0 flex-col">
        <span>{label}</span>
        {hint ? <span className="text-[13px] leading-5 text-muted-foreground">{hint}</span> : null}
      </span>
    </button>
  );
}
