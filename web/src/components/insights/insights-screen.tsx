"use client";

import { FunnelPanel } from "@/components/insights/funnel-panel";
import { OutreachPanel } from "@/components/insights/outreach-panel";
import { PageHeader } from "@/components/shell/page-header";

export function InsightsScreen() {
  return (
    <>
      <PageHeader
        title="How it's going."
        description="Where your jobs stand and how your outreach is landing, counted by day in India time. Today's events show up after the nightly update."
      />
      <h2 className="h-display border-b border-ink p-[22px] text-[34px]">Job funnel</h2>
      <FunnelPanel />
      <h2 className="h-display border-b border-ink p-[22px] text-[34px]">Outreach</h2>
      <OutreachPanel />
    </>
  );
}
