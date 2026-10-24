"use client";

import Link from "next/link";

import { NAV_SECTIONS, type NavSection } from "@/content/sections";
import { cn } from "@/lib/utils";

interface SidebarProps {
  current: NavSection | undefined;
  // Pending drafts, shown as a chip on Inbox. Null hides the chip.
  inboxLabel: string | null;
  collapsed: boolean;
  onToggle: () => void;
}

// The numbered section list: 200px wide, 60px collapsed. Below 860px it
// becomes a menu across the top and the collapse control is hidden.
export function Sidebar({ current, inboxLabel, collapsed, onToggle }: SidebarProps) {
  return (
    <aside
      className={cn(
        "flex shrink-0 flex-col border-r border-line max-[860px]:w-auto max-[860px]:border-r-0 max-[860px]:border-b",
        collapsed ? "w-[60px]" : "w-[200px]",
      )}
    >
      <nav aria-label="Shogun" className="flex flex-col max-[860px]:flex-row max-[860px]:flex-wrap">
        {NAV_SECTIONS.map((item, index) => {
          const active = item === current;
          const showChip = item.href === "/inbox" && inboxLabel !== null;
          return (
            <Link
              key={item.href}
              href={item.href}
              aria-current={active ? "page" : undefined}
              aria-label={collapsed ? item.label : undefined}
              className={cn(
                "flex h-11 items-center gap-3 border-l-2 border-transparent px-4 text-text hover:bg-panel max-[860px]:min-h-11 max-[860px]:grow max-[860px]:justify-center max-[860px]:border-l-0 max-[860px]:px-3",
                active && "panel border-scheme max-[860px]:border-b-2 max-[860px]:border-scheme",
              )}
            >
              <span className="label text-muted-foreground">{String(index + 1).padStart(2, "0")}</span>
              <span className={cn("grow", collapsed && "sr-only max-[860px]:not-sr-only")}>{item.label}</span>
              {showChip ? (
                <span
                  data-testid="inbox-count"
                  className="label bg-scheme px-1.5 py-0.5 font-medium text-on-accent"
                >
                  <span className="sr-only">{inboxLabel} waiting</span>
                  <span aria-hidden="true">{inboxLabel}</span>
                </span>
              ) : null}
            </Link>
          );
        })}
      </nav>
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={!collapsed}
        className="label mt-auto h-11 cursor-pointer border-t border-line px-4 text-left text-muted-foreground hover:text-text max-[860px]:hidden"
      >
        {collapsed ? "»" : "« Collapse"}
        <span className="sr-only">{collapsed ? "Expand sidebar" : "Collapse sidebar"}</span>
      </button>
    </aside>
  );
}
