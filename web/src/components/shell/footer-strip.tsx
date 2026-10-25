import type { ReactNode } from "react";

import type { Scheme } from "@/content/sections";

interface FooterStripProps {
  scheme: Scheme;
  // The bell, the owner and Logout, passed in so the strip stays presentational.
  children: ReactNode;
}

const SCHEME_NAMES: Record<Scheme, string> = {
  ember: "Ember",
  moss: "Moss",
  tide: "Tide",
  plum: "Plum",
};

// The strip along the bottom: the page's scheme, the approval promise and the
// owner's utilities.
export function FooterStrip({ scheme, children }: FooterStripProps) {
  return (
    <footer
      aria-label="Utility"
      className="label flex min-h-11 flex-wrap items-center justify-between gap-x-6 gap-y-2 border-t border-line px-5 py-1 text-muted-foreground max-[860px]:px-3"
    >
      <span>Page scheme: {SCHEME_NAMES[scheme]}</span>
      <span>Nothing leaves without your approval</span>
      <div className="flex flex-wrap items-center gap-3 normal-case">{children}</div>
    </footer>
  );
}
