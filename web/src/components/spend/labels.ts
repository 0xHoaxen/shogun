import { BudgetMode, BudgetPeriod, BudgetScope, SpendGroup, type Budget } from "@/gen/shogun/api/v1/costs_pb";

export const SPEND_GROUPS: readonly { group: SpendGroup; label: string }[] = [
  { group: SpendGroup.DAY, label: "Day" },
  { group: SpendGroup.SERVICE, label: "Service" },
  { group: SpendGroup.FEATURE, label: "Feature" },
  { group: SpendGroup.MODEL, label: "Model" },
];

export function groupLabel(group: SpendGroup): string {
  return SPEND_GROUPS.find((entry) => entry.group === group)?.label ?? "Group";
}

// budgetName is what a budget covers: everything, one service or one feature.
export function budgetName(budget: Budget): string {
  return budget.scope === BudgetScope.GLOBAL ? "All services" : budget.scopeValue;
}

export function periodLabel(period: BudgetPeriod): string {
  return period === BudgetPeriod.DAILY ? "Daily" : "Monthly";
}

export function modeLabel(mode: BudgetMode): string {
  return mode === BudgetMode.HARD ? "Hard" : "Soft";
}
