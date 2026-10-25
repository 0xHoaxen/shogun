"use client";

import { useQuery } from "@connectrpc/connect-query";

import { ProviderForm } from "@/components/provider/provider-form";
import { ProviderStatus } from "@/components/provider/provider-status";
import { Button } from "@/components/ui/button";
import { ProviderService } from "@/gen/shogun/api/v1/providers_pb";

// ProviderSettings is the Settings > AI provider screen.
export function ProviderSettings() {
  const query = useQuery(ProviderService.method.getProvider, {});

  if (query.isError) {
    return (
      <div className="flex items-center gap-3 p-[22px]">
        <p role="alert">Couldn&apos;t load your provider.</p>
        <Button onClick={() => query.refetch()}>Retry</Button>
      </div>
    );
  }
  if (!query.data) {
    return <p className="p-[22px] text-muted-foreground">Loading…</p>;
  }
  const provider = query.data.provider;
  return (
    <>
      <ProviderStatus provider={provider} tokensThisMonth={query.data.tokensThisMonth} />
      <ProviderForm key={String(provider?.version ?? 0)} provider={provider} />
    </>
  );
}
