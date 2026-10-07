"use client";

import { useQuery } from "@connectrpc/connect-query";
import { useState } from "react";

import { insightsErrorText } from "@/components/insights/errors";
import { RANGES, percent, rangeFor, type RangeId } from "@/components/insights/format";
import { Toggle } from "@/components/insights/toggle";
import { Button } from "@/components/ui/button";
import { InsightsFunnelGroup, InsightsService, type InsightsFunnelRow } from "@/gen/shogun/api/v1/insights_pb";
import { cn } from "@/lib/utils";

const GROUPS = [
  { id: InsightsFunnelGroup.SOURCE, label: "By source" },
  { id: InsightsFunnelGroup.MONTH, label: "By month" },
] as const;

const HEADINGS = ["", "Added", "Applied", "Shortlisted", "Interviews", "Offers", "Rejected", "Interview rate", "Offer rate"] as const;

export function FunnelPanel() {
  const [range, setRange] = useState<RangeId>("30");
  const [group, setGroup] = useState<InsightsFunnelGroup>(InsightsFunnelGroup.SOURCE);
  const days = RANGES.find((r) => r.id === range)?.days ?? 30;
  const { from, to } = rangeFor(days);

  const funnel = useQuery(InsightsService.method.getInsightsFunnel, { from, to, groupBy: group });
  const rows = funnel.data?.rows ?? [];
  const total = funnel.data?.total;

  return (
    <section aria-label="Funnel">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-ink p-[22px]">
        <div role="group" aria-label="Range" className="flex flex-wrap">
          {RANGES.map((r) => (
            <Toggle key={r.id} pressed={range === r.id} onClick={() => setRange(r.id)}>
              {r.label}
            </Toggle>
          ))}
        </div>
        <div role="group" aria-label="Group funnel" className="flex flex-wrap">
          {GROUPS.map((g) => (
            <Toggle key={g.id} pressed={group === g.id} onClick={() => setGroup(g.id)}>
              {g.label}
            </Toggle>
          ))}
        </div>
      </div>
      {funnel.isError ? (
        <div className="flex items-center gap-3 border-b border-ink p-[22px]">
          <p role="alert">{insightsErrorText(funnel.error, "Couldn't load the funnel.")}</p>
          <Button onClick={() => funnel.refetch()}>Retry</Button>
        </div>
      ) : (
        <div className="overflow-x-auto border-b border-ink">
          <table className="w-full min-w-[860px] border-collapse text-left">
            <caption className="sr-only">Job funnel</caption>
            <thead>
              <tr className="label h-9 bg-ink text-paper">
                {HEADINGS.map((heading, index) => (
                  <th key={`${heading}-${index}`} className="px-2.5 font-medium">
                    {heading}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <FunnelLine key={row.key} row={row} />
              ))}
              {total && rows.length > 0 ? <FunnelLine row={total} strong /> : null}
            </tbody>
          </table>
          {funnel.isSuccess && rows.length === 0 ? (
            <p className="p-[22px] text-muted-ink">Nothing yet in this range. Today&apos;s events show up after tonight&apos;s update.</p>
          ) : null}
        </div>
      )}
    </section>
  );
}

function FunnelLine({ row, strong = false }: { row: InsightsFunnelRow; strong?: boolean }) {
  return (
    <tr className={cn("border-b border-ink", strong && "bg-strip font-bold")}>
      <th scope="row" className="px-2.5 py-2.5 text-left font-medium">
        {strong ? "All" : row.key}
      </th>
      <td className="px-2.5 py-2.5">{row.jobsAdded.toString()}</td>
      <td className="px-2.5 py-2.5">{row.applications.toString()}</td>
      <td className="px-2.5 py-2.5">{row.shortlisted.toString()}</td>
      <td className="px-2.5 py-2.5">{row.interviews.toString()}</td>
      <td className="px-2.5 py-2.5">{row.offers.toString()}</td>
      <td className="px-2.5 py-2.5">{row.rejections.toString()}</td>
      <td className="px-2.5 py-2.5">{Number(row.applications) > 0 ? percent(row.interviewRate) : "-"}</td>
      <td className="px-2.5 py-2.5">{Number(row.applications) > 0 ? percent(row.offerRate) : "-"}</td>
    </tr>
  );
}
