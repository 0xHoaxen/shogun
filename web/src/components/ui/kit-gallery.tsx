"use client";

import { useState } from "react";

import { FilterChips } from "@/components/ui/filter-chips";
import { ProgressLines } from "@/components/ui/progress-lines";
import { Segmented } from "@/components/ui/segmented";
import { SidePanel } from "@/components/ui/side-panel";
import { StepHeader } from "@/components/ui/step-header";
import { Tag } from "@/components/ui/tag";
import { useToast } from "@/components/ui/toast";
import { Toggle } from "@/components/ui/toggle";
import { Button } from "@/components/ui/button";

type Relationship = "all" | "recruiter" | "friend";
type Density = "comfortable" | "compact";

export function KitGallery() {
  const toast = useToast();
  const [panel, setPanel] = useState(false);
  const [chip, setChip] = useState<Relationship>("all");
  const [density, setDensity] = useState<Density>("comfortable");
  const [on, setOn] = useState(false);
  const [undone, setUndone] = useState(false);

  return (
    <div className="flex flex-col gap-8 p-[22px]">
      <h1 className="h-display text-5xl">Kit</h1>

      <section aria-label="Panel and toast" className="flex flex-wrap items-center gap-3">
        <Button onClick={() => setPanel(true)}>Open panel</Button>
        <Button onClick={() => toast.show({ text: "5 people imported", undo: () => setUndone(true) })}>Show toast</Button>
        {undone ? <span>Undone</span> : null}
      </section>
      <SidePanel open={panel} onOpenChange={setPanel} eyebrow="Bulk import">
        <p>Panel body</p>
        <Button>Inside</Button>
      </SidePanel>

      <FilterChips
        label="Relationship"
        value={chip}
        onChange={setChip}
        chips={[
          { value: "all", label: "All", count: 12 },
          { value: "recruiter", label: "Recruiter", count: 5 },
          { value: "friend", label: "Friend", count: 7 },
        ]}
      />
      <Segmented
        label="Density"
        value={density}
        onChange={setDensity}
        options={[
          { value: "comfortable", label: "Comfortable" },
          { value: "compact", label: "Compact" },
        ]}
      />
      <Toggle checked={on} onCheckedChange={setOn} label="Undo window" hint="Hold sends for 10 seconds." />
      <div className="flex gap-2">
        <Tag>Default</Tag>
        <Tag tone="ok">Ok</Tag>
        <Tag tone="warn">Check</Tag>
      </div>
      <StepHeader steps={["Choose file", "AI reads it", "Check and import"]} current={1} />
      <ProgressLines
        lines={[
          { text: "Reading the file", state: "done" },
          { text: "Matching columns", state: "now" },
          { text: "Checking for duplicates", state: "pending" },
        ]}
      />
    </div>
  );
}
