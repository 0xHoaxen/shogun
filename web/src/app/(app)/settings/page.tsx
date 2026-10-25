import { redirect } from "next/navigation";

import { PageHeader } from "@/components/shell/page-header";
import { SECTIONS } from "@/content/sections";
import { SettingsOverview } from "@/components/settings/settings-overview";

interface SettingsPageProps {
  searchParams: Promise<{ tab?: string }>;
}

export default async function SettingsPage({ searchParams }: SettingsPageProps) {
  // V1.2 briefly served Discovery as a query on this page.
  if ((await searchParams).tab === "discovery") {
    redirect("/settings/discovery");
  }
  return (
    <>
      <PageHeader about={SECTIONS.settings}
        title="Your keys and connections."
        description="Everything Shogun needs from you lives here: the accounts you connect, what your AI calls may spend, and how Shogun reaches you."
      />
      <SettingsOverview />
    </>
  );
}
