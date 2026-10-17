"use client";

import { useQuery } from "@connectrpc/connect-query";
import Link from "next/link";

import { ProviderService } from "@/gen/shogun/api/v1/providers_pb";

// AiProviderNotice tells a user who has not added an AI provider why this
// page's AI features do nothing yet, and where to fix it. It stays out of the
// way while loading, on a failed load, and once a provider is set.
export function AiProviderNotice() {
  const provider = useQuery(ProviderService.method.getProvider, {});
  if (!provider.isSuccess || provider.data.provider) {
    return null;
  }
  return (
    <div role="note" data-testid="ai-provider-notice" className="border-b border-ink bg-strip px-[22px] py-3">
      AI features are off until you add your AI provider.{" "}
      <Link href="/settings/provider" className="font-bold underline">
        Add it in Settings
      </Link>
      .
    </div>
  );
}
