"use client";

import { Dialog as DialogPrimitive } from "@base-ui/react/dialog";
import type { ReactNode } from "react";

interface SidePanelProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  // The small uppercase label that names the panel, for example "Bulk import".
  // It is also the panel's accessible name.
  eyebrow: string;
  children: ReactNode;
}

// A panel that slides in from the right over a scrim. Esc, the scrim and the
// close button dismiss it; focus stays inside while it is open and returns to
// the control that opened it.
export function SidePanel({ open, onOpenChange, eyebrow, children }: SidePanelProps) {
  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Backdrop className="fixed inset-0 z-40 bg-bg/60 transition-opacity duration-300 data-[ending-style]:opacity-0 data-[starting-style]:opacity-0" />
        <DialogPrimitive.Popup
          className="fixed top-0 right-0 bottom-0 z-41 flex w-[460px] max-w-full flex-col overflow-y-auto border-l border-dashed border-line2 bg-panel-solid text-text backdrop-blur-lg transition-transform duration-[380ms] ease-out data-[ending-style]:translate-x-full data-[starting-style]:translate-x-full"
        >
          <div className="sticky top-0 z-10 flex items-center gap-2 border-b border-line bg-bg/60 py-3 pr-4 pl-7 backdrop-blur-md">
            <DialogPrimitive.Title className="label mr-auto text-muted-foreground">{eyebrow}</DialogPrimitive.Title>
            <DialogPrimitive.Close
              aria-label="Close"
              className="inline-flex size-8 cursor-pointer items-center justify-center border border-line2 text-muted-foreground hover:border-scheme hover:bg-scheme hover:text-on-accent"
            >
              <span aria-hidden="true">×</span>
            </DialogPrimitive.Close>
          </div>
          <div className="flex flex-col gap-[18px] p-7 max-[860px]:px-4 max-[860px]:py-5">{children}</div>
        </DialogPrimitive.Popup>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}
