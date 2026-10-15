import { PageHeader } from "@/components/shell/page-header";
import { SECTIONS } from "@/content/sections";
import { SettingsOverview } from "@/components/settings/settings-overview";

export default function SettingsPage() {
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
