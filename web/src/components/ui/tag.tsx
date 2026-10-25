import type { ReactNode } from "react";

import { cn } from "@/lib/utils";

export type TagTone = "default" | "ok" | "warn";

interface TagProps {
  tone?: TagTone;
  children: ReactNode;
}

const TONE_CLASS: Record<TagTone, string> = {
  default: "text-text",
  ok: "text-scheme before:bg-current",
  // Amber, with a square bullet so the meaning does not rely on colour alone.
  warn: "text-[#d8b35a] before:bg-current",
};

// A small mono label in a 1px box.
export function Tag({ tone = "default", children }: TagProps) {
  return (
    <span
      className={cn(
        "label inline-flex min-h-5 items-center gap-1.5 border border-line2 px-1.5 whitespace-nowrap",
        tone !== "default" && "before:size-1.5 before:content-['']",
        TONE_CLASS[tone],
      )}
    >
      {children}
    </span>
  );
}
