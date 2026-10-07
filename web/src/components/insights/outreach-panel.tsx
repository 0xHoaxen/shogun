"use client";

import { useQuery } from "@connectrpc/connect-query";
import { useState } from "react";

import { insightsErrorText } from "@/components/insights/errors";
import { RANGES, percent, rangeFor, type RangeId } from "@/components/insights/format";
import { Toggle } from "@/components/insights/toggle";
import { Button } from "@/components/ui/button";
import { InsightsOutreachGroup, InsightsService, type InsightsOutreachRow } from "@/gen/shogun/api/v1/insights_pb";

const GROUPS = [
  { id: InsightsOutreachGroup.CHANNEL, label: "By channel" },
  { id: InsightsOutreachGroup.STATUS, label: "By status" },
] as const;

export function OutreachPanel() {
  const [range, setRange] = useState<RangeId>("30");
  const [group, setGroup] = useState<InsightsOutreachGroup>(InsightsOutreachGroup.CHANNEL);
  const days = RANGES.find((r) => r.id === range)?.days ?? 30;
  const { from, to } = rangeFor(days);

  const outreach = useQuery(InsightsService.method.getInsightsOutreach, { from, to, groupBy: group });
  const rows = outreach.data?.rows ?? [];
  const total = outreach.data?.total;
  const byChannel = group === InsightsOutreachGroup.CHANNEL;

  return (
    <section aria-label="Outreach">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-ink p-[22px]">
        <div role="group" aria-label="Outreach range" className="flex flex-wrap">
          {RANGES.map((r) => (
            <Toggle key={r.id} pressed={range === r.id} onClick={() => setRange(r.id)}>
              {r.label}
            </Toggle>
          ))}
        </div>
        <div role="group" aria-label="Group outreach" className="flex flex-wrap">
          {GROUPS.map((g) => (
            <Toggle key={g.id} pressed={group === g.id} onClick={() => setGroup(g.id)}>
              {g.label}
            </Toggle>
          ))}
        </div>
      </div>
      {outreach.isError ? (
        <div className="flex items-center gap-3 border-b border-ink p-[22px]">
          <p role="alert">{insightsErrorText(outreach.error, "Couldn't load outreach.")}</p>
          <Button onClick={() => outreach.refetch()}>Retry</Button>
        </div>
      ) : (
        <div className="overflow-x-auto border-b border-ink">
          <table className="w-full min-w-[560px] border-collapse text-left">
            <caption className="sr-only">Outreach</caption>
            <thead>
              <tr className="label h-9 bg-ink text-paper">
                <th className="px-2.5 font-medium">{byChannel ? "Channel" : "Status"}</th>
                {byChannel ? (
                  <>
                    <th className="px-2.5 font-medium">Contacted</th>
                    <th className="px-2.5 font-medium">Replied</th>
                    <th className="px-2.5 font-medium">Reply rate</th>
                  </>
                ) : (
                  <th className="px-2.5 font-medium">Moved into</th>
                )}
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <OutreachLine key={row.key} row={row} byChannel={byChannel} />
              ))}
            </tbody>
          </table>
          {outreach.isSuccess && rows.length === 0 ? (
            <p className="p-[22px] text-muted-ink">Nothing yet in this range.</p>
          ) : null}
          {total && byChannel && rows.length > 0 ? (
            <p className="border-t border-ink bg-strip p-[22px] font-bold">
              All channels: {total.sent.toString()} contacted, {total.replied.toString()} replied (
              {Number(total.sent) > 0 ? percent(total.replyRate) : "-"})
            </p>
          ) : null}
          {total && !byChannel && rows.length > 0 ? (
            <p className="border-t border-ink bg-strip p-[22px] font-bold">
              Overall: {total.sent.toString()} contacted, {total.replied.toString()} replied (
              {Number(total.sent) > 0 ? percent(total.replyRate) : "-"})
            </p>
          ) : null}
        </div>
      )}
    </section>
  );
}

function OutreachLine({ row, byChannel }: { row: InsightsOutreachRow; byChannel: boolean }) {
  return (
    <tr className="border-b border-ink">
      <th scope="row" className="px-2.5 py-2.5 text-left font-medium">
        {row.key}
      </th>
      {byChannel ? (
        <>
          <td className="px-2.5 py-2.5">{row.sent.toString()}</td>
          <td className="px-2.5 py-2.5">{row.replied.toString()}</td>
          <td className="px-2.5 py-2.5">{Number(row.sent) > 0 ? percent(row.replyRate) : "-"}</td>
        </>
      ) : (
        <td className="px-2.5 py-2.5">{row.movedIn.toString()}</td>
      )}
    </tr>
  );
}
