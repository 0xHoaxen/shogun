interface RailProps {
  // The rotated page name along the bottom edge, for example "JOB PIPELINE".
  label: string;
}

// The 104px signal rail that frames every page. Below 860px it becomes a 56px bar.
export function Rail({ label }: RailProps) {
  return (
    <aside
      aria-label="Section"
      className="relative overflow-hidden border-r border-ink bg-signal max-[860px]:h-14 max-[860px]:border-r-0 max-[860px]:border-b"
    >
      <div className="absolute top-4 left-3.5 font-display text-[26px] leading-none font-bold tracking-[-0.05em] text-rail-ink max-[860px]:top-1 max-[860px]:text-xl">
        SHOGUN
        <br />将
      </div>
      <div
        aria-hidden="true"
        className="absolute bottom-6 left-1/2 -translate-x-1/2 rotate-180 font-display text-[60px] leading-none font-medium tracking-[-0.04em] whitespace-nowrap text-rail-ink [writing-mode:vertical-rl] max-[860px]:hidden"
      >
        {label}
      </div>
    </aside>
  );
}
