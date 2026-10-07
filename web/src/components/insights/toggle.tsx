import { cn } from "@/lib/utils";

interface ToggleProps {
  pressed: boolean;
  onClick: () => void;
  children: string;
}

// Toggle is one button of a ruled group where exactly one is pressed.
export function Toggle({ pressed, onClick, children }: ToggleProps) {
  return (
    <button
      type="button"
      aria-pressed={pressed}
      onClick={onClick}
      className={cn(
        "label -mr-px -mb-px inline-flex min-h-9 cursor-pointer items-center border border-ink px-3 font-medium hover:bg-strip",
        pressed && "bg-ink text-paper hover:bg-ink",
      )}
    >
      {children}
    </button>
  );
}
