import type { ReactNode } from "react";

import { cn } from "@/lib/utils";

interface ResultLineProps {
  tone: "ok" | "bad";
  children: ReactNode;
  testId?: string;
}

// A dashed box with a square bullet that says how a test went. A failure is an
// alert so it is read out; a pass is a status.
export function ResultLine({ tone, children, testId }: ResultLineProps) {
  return (
    <div
      role={tone === "bad" ? "alert" : "status"}
      data-testid={testId}
      data-tone={tone}
      className={cn(
        "flex items-center gap-2.5 border border-dashed px-3 py-2.5",
        tone === "ok" ? "border-scheme text-text" : "border-[#ee7f70] text-text",
      )}
    >
      <i aria-hidden="true" className={cn("size-1.5 shrink-0", tone === "ok" ? "bg-scheme" : "bg-[#ee7f70]")} />
      <span>{children}</span>
    </div>
  );
}
