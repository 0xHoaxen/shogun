"use client";

import { useQuery } from "@connectrpc/connect-query";
import Link from "next/link";
import { usePathname } from "next/navigation";

import { AuthService } from "@/gen/shogun/api/v1/auth_pb";
import { cn } from "@/lib/utils";

export interface SettingsTab {
  href: string;
  label: string;
  // What the tab is for, shown on the Settings overview.
  summary: string;
  adminOnly?: boolean;
}

// The Settings tabs, in order. The GitHub and Account tabs are added by their
// own tasks.
export const SETTINGS_TABS: readonly SettingsTab[] = [
  {
    href: "/settings/provider",
    label: "AI provider",
    summary: "Add the AI provider, model and key Shogun uses for your drafts, mail sorting and job scoring.",
  },
  {
    href: "/settings/mail",
    label: "Mail",
    summary: "Connect the Gmail account Shogun reads for job mail and sends approved drafts from.",
  },
  {
    href: "/settings/spend",
    label: "Spend",
    summary: "See what your AI calls cost and set the limits that stop them.",
  },
  {
    href: "/settings/notifications",
    label: "Alerts",
    summary: "Choose whether notifications are on and when the daily digest stays quiet.",
  },
  {
    href: "/settings/users",
    label: "Users",
    summary: "Disable accounts and approve who may connect Gmail.",
    adminOnly: true,
  },
];

// useSettingsTabs returns the tabs the signed-in user may open.
export function useSettingsTabs(): readonly SettingsTab[] {
  const session = useQuery(AuthService.method.getSession);
  const isAdmin = session.data?.session?.role === "admin";
  return SETTINGS_TABS.filter((tab) => !tab.adminOnly || isAdmin);
}

// SettingsNav is the tab list down the left of every Settings page.
export function SettingsNav() {
  const pathname = usePathname();
  const tabs = useSettingsTabs();
  return (
    <nav aria-label="Settings" className="flex flex-col border-r border-ink max-[860px]:flex-row max-[860px]:flex-wrap max-[860px]:border-r-0 max-[860px]:border-b">
      {tabs.map((tab) => {
        const active = pathname.startsWith(tab.href);
        return (
          <Link
            key={tab.href}
            href={tab.href}
            aria-current={active ? "page" : undefined}
            className={cn(
              "label inline-flex min-h-11 items-center border-b border-ink px-4 font-medium hover:bg-ink hover:text-paper max-[860px]:grow max-[860px]:justify-center max-[860px]:border-r max-[860px]:border-b-0",
              active && "bg-ink text-paper",
            )}
          >
            {tab.label}
          </Link>
        );
      })}
    </nav>
  );
}
