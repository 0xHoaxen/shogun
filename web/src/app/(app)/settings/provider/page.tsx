import { ProviderSettings } from "@/components/provider/provider-settings";
import { PageHeader } from "@/components/shell/page-header";
import { SECTIONS } from "@/content/sections";

export default function ProviderPage() {
  return (
    <>
      <PageHeader
        about={SECTIONS.provider}
        title="Your AI provider."
        description="Shogun writes drafts, sorts mail and scores jobs with the provider you add here, using your own key. Nothing is shared with anyone else."
      />
      <ProviderSettings />
    </>
  );
}
