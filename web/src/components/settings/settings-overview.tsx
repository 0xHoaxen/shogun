"use client";

import Link from "next/link";

import { useSettingsTabs } from "@/components/settings/settings-nav";

// SettingsOverview lists each Settings tab with what it is for.
export function SettingsOverview() {
  const tabs = useSettingsTabs();
  return (
    <ul aria-label="Settings sections" className="flex flex-col">
      {tabs.map((tab) => (
        <li key={tab.href} className="border-b border-line">
          <Link href={tab.href} className="flex flex-col gap-1 p-[22px] hover:bg-panel">
            <span className="label font-medium">{tab.label}</span>
            <span className="text-muted-foreground">{tab.summary}</span>
          </Link>
        </li>
      ))}
    </ul>
  );
}
