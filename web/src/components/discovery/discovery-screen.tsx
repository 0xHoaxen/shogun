"use client";

import { useState } from "react";

import { PostingsPanel } from "@/components/discovery/postings-panel";
import { PreferencesPanel } from "@/components/discovery/preferences-panel";
import { SourcesPanel } from "@/components/discovery/sources-panel";
import { SettingsCard } from "@/components/settings/settings-card";
import { AiProviderNotice } from "@/components/provider/ai-provider-notice";
import { PageHeader } from "@/components/shell/page-header";
import { SECTIONS } from "@/content/sections";
import { cn } from "@/lib/utils";

type View = "postings" | "sources" | "preferences";

const VIEWS: readonly { id: View; label: string }[] = [
  { id: "postings", label: "Postings" },
  { id: "sources", label: "Sources" },
  { id: "preferences", label: "What you want" },
];

export function DiscoveryScreen() {
  const [view, setView] = useState<View>("postings");
  return (
    <>
      <PageHeader about={SECTIONS.discovery}
        title="What's out there."
        description="Postings from the sources you add, scored against what you are looking for. Save the good ones to your job board."
      />
      <AiProviderNotice />
      <div role="group" aria-label="View" className="flex flex-wrap border-b border-line2 p-[22px] pb-[23px]">
        {VIEWS.map((v) => (
          <button
            key={v.id}
            type="button"
            aria-pressed={view === v.id}
            onClick={() => setView(v.id)}
            className={cn(
              "label -mr-px -mb-px inline-flex min-h-9 cursor-pointer items-center border border-line2 px-3 font-medium hover:bg-panel",
              view === v.id && "bg-cream text-on-accent hover:bg-cream",
            )}
          >
            {v.label}
          </button>
        ))}
      </div>
      {view === "postings" ? (
        <SettingsCard id="postings" label="Postings" note="Scored against what you want">
          <PostingsPanel />
        </SettingsCard>
      ) : null}
      {view === "sources" ? (
        <SettingsCard id="sources" label="Discovery sources" note="Read on a schedule, robots.txt respected">
          <SourcesPanel />
        </SettingsCard>
      ) : null}
      {view === "preferences" ? (
        <SettingsCard id="preferences" label="What you want" note="Used to score every posting">
          <PreferencesPanel />
        </SettingsCard>
      ) : null}
    </>
  );
}
