import type { ReactNode } from "react";

interface SettingsCardProps {
  // Names the card for assistive technology; the heading id derives from it.
  id: string;
  label: string;
  // A short mono note at the right of the header, such as "Every AI call is metered".
  note?: string;
  // The action row along the bottom of a form card.
  footer?: ReactNode;
  children: ReactNode;
}

// The card every Settings tab is made of: a header bar with a label and a note,
// a body, and an optional footer for the actions.
export function SettingsCard({ id, label, note, footer, children }: SettingsCardProps) {
  const headingId = `${id}-heading`;
  return (
    <section aria-labelledby={headingId} className="m-[22px] border border-dashed border-line2 bg-panel">
      <header className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 border-b border-line2 px-[22px] py-3">
        <h2 id={headingId} className="label font-medium">
          {label}
        </h2>
        {note ? <span className="label text-muted-foreground">{note}</span> : null}
      </header>
      <div>{children}</div>
      {footer ? <footer className="border-t border-line2 p-[22px]">{footer}</footer> : null}
    </section>
  );
}
