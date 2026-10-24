import { cn } from "@/lib/utils";

export type LineState = "done" | "now" | "pending";

export interface ProgressLine {
  text: string;
  state: LineState;
}

interface ProgressLinesProps {
  lines: readonly ProgressLine[];
}

const STATE_WORD: Record<LineState, string> = { done: "done", now: "in progress", pending: "waiting" };

// The "AI reads it" list: lines tick over one by one, the line in progress blinks.
export function ProgressLines({ lines }: ProgressLinesProps) {
  return (
    <ul className="flex flex-col gap-2 font-mono text-xs">
      {lines.map((line) => (
        <li
          key={line.text}
          data-state={line.state}
          className={cn("flex items-center gap-2.5", line.state === "done" ? "text-text" : "text-muted-foreground")}
        >
          <i
            aria-hidden="true"
            className={cn(
              "size-2 shrink-0 bg-line2",
              line.state !== "pending" && "bg-scheme",
              line.state === "now" && "animate-[blink_0.8s_steps(2)_infinite]",
            )}
          />
          {line.text}
          <span className="sr-only"> ({STATE_WORD[line.state]})</span>
        </li>
      ))}
    </ul>
  );
}
