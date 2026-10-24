import { cn } from "@/lib/utils";

export interface FilterChip<T extends string> {
  value: T;
  label: string;
  // Shown after the label when set, for example the number of matching rows.
  count?: number;
}

interface FilterChipsProps<T extends string> {
  label: string;
  chips: readonly FilterChip<T>[];
  value: T;
  onChange: (value: T) => void;
}

// A row of chips that act like a single choice. The chosen one is aria-pressed.
export function FilterChips<T extends string>({ label, chips, value, onChange }: FilterChipsProps<T>) {
  return (
    <div role="group" aria-label={label} className="flex flex-wrap gap-2 max-[860px]:flex-nowrap max-[860px]:overflow-x-auto">
      {chips.map((chip) => {
        const on = chip.value === value;
        return (
          <button
            key={chip.value}
            type="button"
            aria-pressed={on}
            onClick={() => onChange(chip.value)}
            className={cn(
              "label inline-flex min-h-7 shrink-0 cursor-pointer items-center gap-1.5 border border-line2 px-2.5 text-muted-foreground hover:text-text max-[860px]:min-h-11",
              on && "border-scheme bg-scheme text-on-accent hover:text-on-accent",
            )}
          >
            {chip.label}
            {chip.count === undefined ? null : <span>{chip.count}</span>}
          </button>
        );
      })}
    </div>
  );
}
