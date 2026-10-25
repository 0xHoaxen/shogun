"use client";

import { useMutation } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { providerQueryKey } from "@/components/provider/provider-form";
import { ResultLine } from "@/components/provider/result-line";
import { failureText, providerErrorText } from "@/components/provider/text";
import { Button } from "@/components/ui/button";
import { ProviderService, type Provider } from "@/gen/shogun/api/v1/providers_pb";

interface StatusProps {
  provider: Provider | undefined;
  tokensThisMonth: bigint;
}

// ProviderStatus says whether AI features are on and what is stored, plus the
// buttons that act on the stored provider.
export function ProviderStatus({ provider, tokensThisMonth }: StatusProps) {
  const queryClient = useQueryClient();
  const [confirming, setConfirming] = useState(false);
  const test = useMutation(ProviderService.method.testProvider);
  const remove = useMutation(ProviderService.method.deleteProvider, {
    onSuccess: () => {
      setConfirming(false);
      test.reset();
      return queryClient.invalidateQueries({ queryKey: providerQueryKey });
    },
  });

  if (!provider) {
    return (
      <section aria-labelledby="provider-status" className="flex flex-col gap-2 border-b border-line p-[22px]">
        <h2 id="provider-status" className="label font-medium">
          Status
        </h2>
        <p data-testid="provider-state">
          <strong>AI features are off.</strong> Set up an AI provider below to use drafts, mail sorting, profile suggestions
          and job scoring. Everything else works without it.
        </p>
      </section>
    );
  }

  return (
    <section aria-labelledby="provider-status" className="flex flex-col gap-3 border-b border-line p-[22px]">
      <h2 id="provider-status" className="label font-medium">
        Status
      </h2>
      <p data-testid="provider-state">
        <strong>AI features are on</strong> with {provider.model} at {provider.baseUrl}. Key ending {provider.keyLast4}.
      </p>
      <p className="text-muted-foreground">
        {tokensThisMonth.toLocaleString("en-US")} tokens used this month
        {provider.monthlyTokenCap === undefined ? "." : ` of ${provider.monthlyTokenCap.toLocaleString("en-US")}.`}
      </p>
      <div className="flex flex-wrap items-center gap-2">
        <Button onClick={() => test.mutate({})} disabled={test.isPending}>
          {test.isPending ? "Testing…" : "Test the saved provider"}
        </Button>
        {confirming ? (
          <>
            <Button variant="primary" onClick={() => remove.mutate({})} disabled={remove.isPending}>
              Yes, remove it
            </Button>
            <Button onClick={() => setConfirming(false)}>Keep it</Button>
          </>
        ) : (
          <Button onClick={() => setConfirming(true)}>Remove</Button>
        )}
      </div>
      <p className="text-muted-foreground">Testing makes one tiny call with the saved provider and counts against your budgets.</p>
      {test.isSuccess && test.data.ok ? (
        <ResultLine tone="ok" testId="saved-test-result">
          It works. {test.data.model} replied in {Number(test.data.latencyMs)} ms.
        </ResultLine>
      ) : null}
      {test.isSuccess && !test.data.ok ? (
        <ResultLine tone="bad" testId="saved-test-result">
          {failureText(test.data.failure)}
        </ResultLine>
      ) : null}
      {test.isError ? <p role="alert">{providerErrorText(test.error, "Couldn't run the test. Try again.")}</p> : null}
      {remove.isError ? <p role="alert">{providerErrorText(remove.error, "Couldn't remove the provider. Try again.")}</p> : null}
    </section>
  );
}
