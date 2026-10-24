interface TopBarProps {
  // What the Next strip says, for example "3 drafts waiting for your approval".
  status: string;
}

// The 64px bar across the top: the logo mark and the Next strip, a label, a
// dotted leader and a line of status text.
export function TopBar({ status }: TopBarProps) {
  return (
    <header className="flex h-16 items-center gap-6 border-b border-line px-5 max-[860px]:gap-3 max-[860px]:px-3">
      <div className="flex shrink-0 items-center gap-2.5">
        <span aria-hidden="true" className="font-mark text-2xl leading-none font-bold text-scheme">
          将
        </span>
        <span className="text-base font-semibold tracking-[-0.02em]">Shogun</span>
      </div>
      <div className="flex min-w-0 grow items-center gap-3" data-testid="next-strip">
        <span className="label shrink-0 text-muted-foreground">Next</span>
        <span aria-hidden="true" className="min-w-6 grow border-b border-dotted border-line2" />
        <span className="truncate text-text">{status}</span>
      </div>
    </header>
  );
}
