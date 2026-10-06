import { BudgetsTable } from "@/components/spend/budgets-table";
import { SpendTable } from "@/components/spend/spend-table";
import { PageHeader } from "@/components/shell/page-header";

export function SpendScreen() {
  return (
    <>
      <PageHeader
        title="What it costs."
        description="Every Claude call is metered. Hard budgets stop new calls at their limit; soft ones only tell you."
      />
      <SpendTable />
      <BudgetsTable />
    </>
  );
}
