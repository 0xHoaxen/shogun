import { PageHeader } from "@/components/shell/page-header";
import { SettingsOverview } from "@/components/settings/settings-overview";

export default function SettingsPage() {
  return (
    <>
      <PageHeader
        title="Your keys and connections."
        description="Everything Shogun needs from you lives here: the accounts you connect, what your AI calls may spend, and how Shogun reaches you."
      />
      <SettingsOverview />
    </>
  );
}
