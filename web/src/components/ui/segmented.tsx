import { cn } from "@/lib/utils";

export interface SegmentOption<T extends string> {
  value: T;
  label: string;
}

interface SegmentedProps<T extends string> {
  label: string;
  options: readonly SegmentOption<T>[];
  value: T;
  onChange: (value: T) => void;
}

// A joined row of buttons that act like a single choice; the chosen one is
// aria-pressed and filled cream.
export function Segmented<T extends string>({ label, options, value, onChange }: SegmentedProps<T>) {
  return (
    <div role="group" aria-label={label} className="inline-flex max-w-full self-start border border-line2">
      {options.map((option) => {
        const on = option.value === value;
        return (
          <button
            key={option.value}
            type="button"
            aria-pressed={on}
            onClick={() => onChange(option.value)}
            className={cn(
              "label min-h-7 cursor-pointer border-r border-line2 bg-bg/90 px-2.5 text-muted-foreground last:border-r-0 max-[860px]:min-h-11",
              on && "bg-cream text-on-accent",
            )}
          >
            {option.label}
          </button>
        );
      })}
    </div>
  );
}
