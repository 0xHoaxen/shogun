"use client";

import { useState } from "react";

import { PostingsPanel } from "@/components/discovery/postings-panel";
import { PreferencesPanel } from "@/components/discovery/preferences-panel";
import { SourcesPanel } from "@/components/discovery/sources-panel";
import { PageHeader } from "@/components/shell/page-header";
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
      <PageHeader
        title="What's out there."
        description="Postings from the sources you add, scored against what you are looking for. Save the good ones to your job board."
      />
      <div role="group" aria-label="View" className="flex flex-wrap border-b border-ink p-[22px] pb-[23px]">
        {VIEWS.map((v) => (
          <button
            key={v.id}
            type="button"
            aria-pressed={view === v.id}
            onClick={() => setView(v.id)}
            className={cn(
              "label -mr-px -mb-px inline-flex min-h-9 cursor-pointer items-center border border-ink px-3 font-medium hover:bg-strip",
              view === v.id && "bg-ink text-paper hover:bg-ink",
            )}
          >
            {v.label}
          </button>
        ))}
      </div>
      {view === "postings" ? <PostingsPanel /> : null}
      {view === "sources" ? <SourcesPanel /> : null}
      {view === "preferences" ? <PreferencesPanel /> : null}
    </>
  );
}
