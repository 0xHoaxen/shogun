import { DiscoveryScreen } from "@/components/discovery/discovery-screen";
import { PageHeader } from "@/components/shell/page-header";
import { SECTIONS } from "@/content/sections";
import { SettingsOverview } from "@/components/settings/settings-overview";

interface SettingsPageProps {
  searchParams: Promise<{ tab?: string }>;
}

export default async function SettingsPage({ searchParams }: SettingsPageProps) {
  const { tab } = await searchParams;
  // The Discovery tab replaces this in V0.2; until then /discovery redirects here.
  if (tab === "discovery") {
    return <DiscoveryScreen />;
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
