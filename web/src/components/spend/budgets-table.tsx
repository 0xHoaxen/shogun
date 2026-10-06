"use client";

import { createConnectQueryKey, useMutation, useQuery } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { budgetName, modeLabel, periodLabel } from "@/components/spend/labels";
import { Button } from "@/components/ui/button";
import { TextField } from "@/components/ui/field";
import { BudgetMode, CostsService, type Budget } from "@/gen/shogun/api/v1/costs_pb";
import { REASON_VERSION_CONFLICT, reasonOf } from "@/lib/errors";
import { dollarsInput, formatMicros, parseDollars } from "@/lib/money";

const budgetsQueryKey = createConnectQueryKey({
  schema: CostsService.method.listBudgets,
  cardinality: "finite",
});

const PERCENT = BigInt(100);

function usedPercent(budget: Budget): string {
  if (budget.limitMicros === BigInt(0)) {
    return "-";
  }
  return `${(budget.spentMicros * PERCENT) / budget.limitMicros}%`;
}

interface EditRowProps {
  budget: Budget;
  onDone: () => void;
}

// EditRow edits the limit, mode and enabled flag of one budget in place.
function EditRow({ budget, onDone }: EditRowProps) {
  const queryClient = useQueryClient();
  const [limit, setLimit] = useState(() => dollarsInput(budget.limitMicros));
  const [mode, setMode] = useState(budget.mode);
  const [enabled, setEnabled] = useState(budget.enabled);
  const name = budgetName(budget);

  const update = useMutation(CostsService.method.updateBudget, {
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: budgetsQueryKey });
      onDone();
    },
  });

  const limitMicros = parseDollars(limit);
  const canSave = limitMicros !== null && !update.isPending;
  const conflict = update.isError && reasonOf(update.error) === REASON_VERSION_CONFLICT;

  return (
    <tr className="border-b border-ink bg-strip">
      <td colSpan={6} className="p-[22px]">
        <form
          aria-label={`Edit ${name} budget`}
          className="flex flex-wrap items-end gap-4"
          onSubmit={(event) => {
            event.preventDefault();
            if (limitMicros !== null) {
              update.mutate({
                id: budget.id,
                limitMicros,
                mode,
                thresholds: budget.thresholds,
                enabled,
                version: budget.version,
              });
            }
          }}
        >
          <TextField
            label="Limit (USD)"
            inputMode="decimal"
            value={limit}
            onChange={(event) => setLimit(event.target.value)}
            aria-invalid={limitMicros === null}
          />
          <div>
            <label htmlFor={`mode-${budget.id}`} className="label mb-1.5 block text-[11px] font-medium">
              Mode
            </label>
            <select
              id={`mode-${budget.id}`}
              className="min-h-10 border border-ink bg-field px-2.5 text-xs text-ink"
              value={mode}
              onChange={(event) => setMode(Number(event.target.value) as BudgetMode)}
            >
              <option value={BudgetMode.HARD}>Hard: blocks calls at the limit</option>
              <option value={BudgetMode.SOFT}>Soft: only notifies</option>
            </select>
          </div>
          <label className="label flex min-h-10 items-center gap-2 text-[11px] font-medium">
            <input type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} />
            Enabled
          </label>
          <div className="flex gap-2">
            <Button type="submit" variant="primary" disabled={!canSave}>
              Save
            </Button>
            <Button type="button" onClick={onDone}>
              Cancel
            </Button>
          </div>
        </form>
        {limitMicros === null ? (
          <p className="mt-3 text-muted-ink">Enter an amount in dollars, such as 20 or 12.50.</p>
        ) : null}
        {update.isError ? (
          <p role="alert" className="mt-3">
            {conflict
              ? "This budget changed elsewhere. Cancel and try again."
              : "Couldn't save the budget. Try again."}
          </p>
        ) : null}
      </td>
    </tr>
  );
}

export function BudgetsTable() {
  const budgets = useQuery(CostsService.method.listBudgets, {});
  const [editingId, setEditingId] = useState<string | null>(null);
  const rows = budgets.data?.budgets ?? [];

  return (
    <section aria-labelledby="budgets-heading">
      <h2 id="budgets-heading" className="label border-b border-ink bg-strip px-[22px] py-3 font-medium">
        Budgets
      </h2>
      {budgets.isError ? (
        <div className="flex items-center gap-3 p-[22px]">
          <p role="alert">Couldn&apos;t load your budgets.</p>
          <Button onClick={() => budgets.refetch()}>Retry</Button>
        </div>
      ) : (
        <div className="overflow-x-auto">
          <table aria-label="Budgets" className="w-full min-w-[860px] border-collapse text-left">
            <thead>
              <tr className="label h-9 bg-ink text-paper">
                <th className="px-2.5 font-medium">Covers</th>
                <th className="px-2.5 font-medium">Period</th>
                <th className="px-2.5 font-medium">Mode</th>
                <th className="px-2.5 text-right font-medium">Spent</th>
                <th className="px-2.5 text-right font-medium">Limit</th>
                <th className="px-2.5 text-right font-medium">
                  <span className="sr-only">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.flatMap((budget) => {
                const name = budgetName(budget);
                const row = (
                  <tr key={budget.id} className="h-11 border-b border-ink">
                    <td className="px-2.5">
                      <b>{name}</b>
                      {budget.enabled ? null : <span className="ml-2 text-[11px] text-muted-ink">off</span>}
                    </td>
                    <td className="px-2.5">{periodLabel(budget.period)}</td>
                    <td className="px-2.5">{modeLabel(budget.mode)}</td>
                    <td className="px-2.5 text-right">
                      {formatMicros(budget.spentMicros)} ({usedPercent(budget)})
                    </td>
                    <td className="px-2.5 text-right">{formatMicros(budget.limitMicros)}</td>
                    <td className="px-2.5 text-right">
                      <Button aria-label={`Edit ${name} ${periodLabel(budget.period)}`} onClick={() => setEditingId(budget.id)}>
                        Edit
                      </Button>
                    </td>
                  </tr>
                );
                return editingId === budget.id
                  ? [row, <EditRow key={`${budget.id}-edit`} budget={budget} onDone={() => setEditingId(null)} />]
                  : [row];
              })}
            </tbody>
          </table>
          {budgets.isSuccess && rows.length === 0 ? <p className="p-[22px] text-muted-ink">No budgets yet.</p> : null}
        </div>
      )}
    </section>
  );
}
