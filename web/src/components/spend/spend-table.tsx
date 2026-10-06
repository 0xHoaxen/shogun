"use client";

import { useQuery } from "@connectrpc/connect-query";
import { useState } from "react";

import { SPEND_GROUPS, groupLabel } from "@/components/spend/labels";
import { Button } from "@/components/ui/button";
import { TextField } from "@/components/ui/field";
import { CostsService, SpendGroup } from "@/gen/shogun/api/v1/costs_pb";
import { monthStart, ownerToday } from "@/lib/days";
import { formatMicros } from "@/lib/money";
import { cn } from "@/lib/utils";

export function SpendTable() {
  const [today] = useState(() => ownerToday(new Date()));
  const [fromDay, setFromDay] = useState(() => monthStart(today));
  const [toDay, setToDay] = useState(today);
  const [group, setGroup] = useState<SpendGroup>(SpendGroup.DAY);

  const rangeReady = fromDay !== "" && toDay !== "";
  const spend = useQuery(
    CostsService.method.getSpend,
    { fromDay, toDay, groupBy: group },
    { enabled: rangeReady },
  );
  const rows = spend.data?.rows ?? [];

  return (
    <section aria-labelledby="spend-heading" className="border-b border-ink">
      <h2 id="spend-heading" className="label border-b border-ink bg-strip px-[22px] py-3 font-medium">
        Spend
      </h2>
      <div className="flex flex-wrap items-end gap-4 border-b border-ink p-[22px]">
        <TextField label="From" type="date" value={fromDay} onChange={(e) => setFromDay(e.target.value)} />
        <TextField label="To" type="date" value={toDay} onChange={(e) => setToDay(e.target.value)} />
        <div role="group" aria-label="Group by" className="flex">
          {SPEND_GROUPS.map((entry) => (
            <button
              key={entry.group}
              type="button"
              aria-pressed={group === entry.group}
              onClick={() => setGroup(entry.group)}
              className={cn(
                "label -mr-px inline-flex min-h-10 cursor-pointer items-center border border-ink px-3 font-medium hover:bg-strip",
                group === entry.group && "bg-ink text-paper hover:bg-ink",
              )}
            >
              {entry.label}
            </button>
          ))}
        </div>
      </div>

      {spend.isError ? (
        <div className="flex items-center gap-3 p-[22px]">
          <p role="alert">Couldn&apos;t load your spend.</p>
          <Button onClick={() => spend.refetch()}>Retry</Button>
        </div>
      ) : (
        <div className="overflow-x-auto">
          <table aria-label="Spend" className="w-full min-w-[640px] border-collapse text-left">
            <thead>
              <tr className="label h-9 bg-ink text-paper">
                <th className="px-2.5 font-medium">{groupLabel(group)}</th>
                <th className="px-2.5 text-right font-medium">Cost</th>
                <th className="px-2.5 text-right font-medium">Input tokens</th>
                <th className="px-2.5 text-right font-medium">Output tokens</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.key} className="h-11 border-b border-ink">
                  <td className="px-2.5">{row.key}</td>
                  <td className="px-2.5 text-right">{formatMicros(row.costMicros)}</td>
                  <td className="px-2.5 text-right">{row.inputTokens.toLocaleString("en-US")}</td>
                  <td className="px-2.5 text-right">{row.outputTokens.toLocaleString("en-US")}</td>
                </tr>
              ))}
            </tbody>
            <tfoot>
              <tr className="h-11 font-bold">
                <td className="px-2.5">Total</td>
                <td data-testid="spend-total" className="px-2.5 text-right">
                  {formatMicros(spend.data?.totalMicros ?? BigInt(0))}
                </td>
                <td colSpan={2} />
              </tr>
            </tfoot>
          </table>
          {spend.isSuccess && rows.length === 0 ? (
            <p className="p-[22px] text-muted-ink">Nothing spent in this range.</p>
          ) : null}
        </div>
      )}
    </section>
  );
}
