import { cn } from "@/lib/utils";

interface StepHeaderProps {
  // The step names in order, for example ["Choose file", "AI reads it", "Check and import"].
  steps: readonly string[];
  // Zero-based index of the step in progress.
  current: number;
}

// The numbered steps above a multi-step panel. Finished steps are accented,
// the current one is underlined.
export function StepHeader({ steps, current }: StepHeaderProps) {
  return (
    <ol className="flex flex-wrap gap-x-[18px] gap-y-1.5">
      {steps.map((name, index) => {
        const state = index < current ? "done" : index === current ? "current" : "pending";
        return (
          <li
            key={name}
            aria-current={state === "current" ? "step" : undefined}
            data-state={state}
            className={cn(
              "label border-b-2 border-transparent py-0.5 text-muted-foreground",
              state === "done" && "text-scheme",
              state === "current" && "border-scheme text-text",
            )}
          >
            {index + 1} {name}
            {state === "done" ? <span className="sr-only"> (done)</span> : null}
          </li>
        );
      })}
    </ol>
  );
}
