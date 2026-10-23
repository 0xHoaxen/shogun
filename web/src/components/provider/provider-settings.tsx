"use client";

import { createConnectQueryKey, useMutation, useQuery } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { failureText, providerErrorText } from "@/components/provider/text";
import { Button } from "@/components/ui/button";
import { TextField } from "@/components/ui/field";
import { ProviderService, type Provider } from "@/gen/shogun/api/v1/providers_pb";
import { dollarsInput, parseDollars } from "@/lib/money";

const providerQueryKey = createConnectQueryKey({
  schema: ProviderService.method.getProvider,
  cardinality: "finite",
});

const WHOLE_NUMBER = /^\d+$/;

// A field the user may leave empty: undefined for empty, null for a bad entry.
function optional<T>(text: string, parse: (text: string) => T | null): T | undefined | null {
  return text.trim() === "" ? undefined : parse(text);
}

function parseCap(text: string): bigint | null {
  const trimmed = text.trim();
  if (!WHOLE_NUMBER.test(trimmed)) {
    return null;
  }
  const value = BigInt(trimmed);
  return value > BigInt(0) ? value : null;
}

interface FormProps {
  provider: Provider | undefined;
}

// ProviderForm edits the saved provider, or starts one. The parent keys it by
// version, so a save starts it again from what is stored, with the key field
// empty: the key goes in once and is never shown again.
function ProviderForm({ provider }: FormProps) {
  const queryClient = useQueryClient();
  const [baseUrl, setBaseUrl] = useState(provider?.baseUrl ?? "");
  const [model, setModel] = useState(provider?.model ?? "");
  const [apiKey, setApiKey] = useState("");
  const [inputPrice, setInputPrice] = useState(
    provider?.inputMicrosPerMtok === undefined ? "" : dollarsInput(provider.inputMicrosPerMtok),
  );
  const [outputPrice, setOutputPrice] = useState(
    provider?.outputMicrosPerMtok === undefined ? "" : dollarsInput(provider.outputMicrosPerMtok),
  );
  const [cap, setCap] = useState(provider?.monthlyTokenCap === undefined ? "" : provider.monthlyTokenCap.toString());

  const save = useMutation(ProviderService.method.setProvider, {
    onSettled: () => queryClient.invalidateQueries({ queryKey: providerQueryKey }),
  });

  const input = optional(inputPrice, parseDollars);
  const output = optional(outputPrice, parseDollars);
  const limit = optional(cap, parseCap);
  const problem =
    input === null || output === null
      ? "Enter prices in dollars, such as 3 or 0.25."
      : (input === undefined) !== (output === undefined)
        ? "Enter both prices, input and output, or leave both empty."
        : limit === null
          ? "The monthly token limit must be a whole number above 0, or empty for no limit."
          : null;

  return (
    <form
      aria-label="AI provider"
      className="flex max-w-xl flex-col gap-5 p-[22px]"
      onSubmit={(event) => {
        event.preventDefault();
        if (problem || input === null || output === null || limit === null) {
          return;
        }
        save.mutate({
          baseUrl: baseUrl.trim(),
          model: model.trim(),
          apiKey,
          inputMicrosPerMtok: input,
          outputMicrosPerMtok: output,
          monthlyTokenCap: limit,
          version: provider?.version ?? 0,
        });
      }}
    >
      <TextField
        label="Address"
        type="url"
        value={baseUrl}
        placeholder="https://api.openai.com/v1"
        autoComplete="off"
        spellCheck={false}
        onChange={(event) => setBaseUrl(event.target.value)}
      />
      <TextField
        label="Model"
        value={model}
        placeholder="claude-sonnet-5-5"
        autoComplete="off"
        spellCheck={false}
        onChange={(event) => setModel(event.target.value)}
      />
      <div>
        <TextField
          label="API key"
          type="password"
          value={apiKey}
          placeholder={provider ? `Keep the key ending ${provider.keyLast4}` : "Paste your key"}
          autoComplete="off"
          spellCheck={false}
          onChange={(event) => setApiKey(event.target.value)}
        />
        <p className="mt-1.5 text-muted-ink">
          {provider
            ? "Leave empty to keep the stored key. A key is never shown again."
            : "Stored encrypted. It is never shown again, and only used for your own calls."}
        </p>
      </div>
      <fieldset className="flex flex-col gap-3 border border-ink p-3.5">
        <legend className="label px-1.5 font-medium">Optional</legend>
        <div className="flex flex-wrap gap-4">
          <TextField
            label="Input price ($ per million tokens)"
            inputMode="decimal"
            value={inputPrice}
            onChange={(event) => setInputPrice(event.target.value)}
          />
          <TextField
            label="Output price ($ per million tokens)"
            inputMode="decimal"
            value={outputPrice}
            onChange={(event) => setOutputPrice(event.target.value)}
          />
        </div>
        <p className="text-muted-ink">
          Only for a model Shogun has no price for. With no price its usage is counted in tokens and costs nothing against
          your dollar budgets.
        </p>
        <TextField
          label="Monthly token limit"
          inputMode="numeric"
          value={cap}
          placeholder="No limit"
          onChange={(event) => setCap(event.target.value)}
        />
        <p className="text-muted-ink">AI calls stop once the month&apos;s tokens would pass this. Empty means no limit.</p>
      </fieldset>
      {problem ? <p role="alert">{problem}</p> : null}
      {save.isError ? <p role="alert">{providerErrorText(save.error, "Couldn't save the provider. Try again.")}</p> : null}
      {save.isSuccess ? <p role="status">Saved.</p> : null}
      <div>
        <Button type="submit" variant="primary" disabled={problem !== null || save.isPending}>
          Save
        </Button>
      </div>
    </form>
  );
}

interface StatusProps {
  provider: Provider | undefined;
  tokensThisMonth: bigint;
}

// ProviderStatus says whether AI features are on and what is stored, plus the
// buttons that act on the stored provider.
function ProviderStatus({ provider, tokensThisMonth }: StatusProps) {
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
      <section aria-labelledby="provider-status" className="flex flex-col gap-2 border-b border-ink p-[22px]">
        <h2 id="provider-status" className="label font-medium">
          Status
        </h2>
        <p data-testid="provider-state">
          <strong>AI features are off.</strong> Add your provider below to turn on drafts, mail sorting, profile suggestions
          and job scoring. Everything else works without it.
        </p>
      </section>
    );
  }

  return (
    <section aria-labelledby="provider-status" className="flex flex-col gap-3 border-b border-ink p-[22px]">
      <h2 id="provider-status" className="label font-medium">
        Status
      </h2>
      <p data-testid="provider-state">
        <strong>AI features are on</strong> with {provider.model} at {provider.baseUrl}. Key ending {provider.keyLast4}.
      </p>
      <p className="text-muted-ink">
        {tokensThisMonth.toLocaleString("en-US")} tokens used this month
        {provider.monthlyTokenCap === undefined ? "." : ` of ${provider.monthlyTokenCap.toLocaleString("en-US")}.`}
      </p>
      <div className="flex flex-wrap items-center gap-2">
        <Button onClick={() => test.mutate({})} disabled={test.isPending}>
          {test.isPending ? "Testing" : "Test connection"}
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
      <p className="text-muted-ink">Testing makes one tiny call with the saved provider and counts against your budgets.</p>
      {test.isSuccess && test.data.ok ? <p role="status">It works. {test.data.model} answered.</p> : null}
      {test.isSuccess && !test.data.ok ? <p role="alert">{failureText(test.data.failure)}</p> : null}
      {test.isError ? <p role="alert">{providerErrorText(test.error, "Couldn't run the test. Try again.")}</p> : null}
      {remove.isError ? <p role="alert">{providerErrorText(remove.error, "Couldn't remove the provider. Try again.")}</p> : null}
    </section>
  );
}

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
    return <p className="p-[22px] text-muted-ink">Loading…</p>;
  }
  const provider = query.data.provider;
  return (
    <>
      <ProviderStatus provider={provider} tokensThisMonth={query.data.tokensThisMonth} />
      <ProviderForm key={String(provider?.version ?? 0)} provider={provider} />
    </>
  );
}
