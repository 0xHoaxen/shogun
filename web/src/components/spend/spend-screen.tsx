import { BudgetsTable } from "@/components/spend/budgets-table";
import { SpendTable } from "@/components/spend/spend-table";
import { PageHeader } from "@/components/shell/page-header";
import { SECTIONS } from "@/content/sections";

export function SpendScreen() {
  return (
    <>
      <PageHeader about={SECTIONS.spend}
        title="What it costs."
        description="Every Claude call is metered. Hard budgets stop new calls at their limit; soft ones only tell you."
      />
      <SpendTable />
      <BudgetsTable />
    </>
  );
}
