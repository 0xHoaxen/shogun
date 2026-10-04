import type { ReactNode } from "react";

interface PageHeaderProps {
  title: string;
  description: string;
  // Actions for the cell, for example an "Add job" button.
  children?: ReactNode;
}

// The headline cell every page opens with: title on the left, one sentence
// and the page's actions on the right.
export function PageHeader({ title, description, children }: PageHeaderProps) {
  return (
    <div className="grid grid-cols-[minmax(0,3fr)_minmax(0,2fr)] items-end gap-6 border-b border-ink p-[22px] pt-8 max-[860px]:grid-cols-1">
      <h1 className="h-display text-[72px] max-[860px]:text-5xl">{title}</h1>
      <div className="max-w-[46ch] text-muted-ink">
        {description}
        {children ? <div className="mt-3.5 flex gap-2">{children}</div> : null}
      </div>
    </div>
  );
}
