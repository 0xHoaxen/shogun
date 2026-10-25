"use client";

import { createConnectQueryKey, useMutation } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { canTest, saveReadiness, sameTyped, trimmed, type SaveReadiness, type Typed } from "@/components/provider/save-rules";
import { ResultLine } from "@/components/provider/result-line";
import { failureText, providerErrorText } from "@/components/provider/text";
import { Button } from "@/components/ui/button";
import { TextField } from "@/components/ui/field";
import { ProviderService, type Provider } from "@/gen/shogun/api/v1/providers_pb";
import { dollarsInput, parseDollars } from "@/lib/money";

export const providerQueryKey = createConnectQueryKey({
  schema: ProviderService.method.getProvider,
  cardinality: "finite",
});

const WHOLE_NUMBER = /^\d+$/;

// A field the user may leave empty: undefined for empty, null for a bad entry.
function optional<T>(text: string, parse: (text: string) => T | null): T | undefined | null {
  return text.trim() === "" ? undefined : parse(text);
}

function parseCap(text: string): bigint | null {
  const trimmedText = text.trim();
  if (!WHOLE_NUMBER.test(trimmedText)) {
    return null;
  }
  const value = BigInt(trimmedText);
  return value > BigInt(0) ? value : null;
}

// How a test of some typed values went. It is only shown while the fields still
// hold exactly those values, so any edit clears it.
type Outcome = { for: Typed } & ({ ok: true; model: string; latencyMs: number } | { ok: false; text: string });

const HINTS: Record<SaveReadiness, string> = {
  ready: "You can save.",
  incomplete: "",
  "needs-test": "Save stays off until the test passes.",
  "needs-key": "Enter the API key again to test the new address or model.",
};

interface FormProps {
  provider: Provider | undefined;
}

// ProviderForm edits the saved provider, or starts one. The parent keys it by
// version, so a save starts it again from what is stored, with the key field
// empty: the key goes in once and is never shown again. A provider is saved only
// after a passing test of the exact address, model and key being saved.
export function ProviderForm({ provider }: FormProps) {
  const queryClient = useQueryClient();
  const [baseUrl, setBaseUrl] = useState(provider?.baseUrl ?? "");
  const [model, setModel] = useState(provider?.model ?? "");
  const [apiKey, setApiKey] = useState("");
  const [showKey, setShowKey] = useState(false);
  const [outcome, setOutcome] = useState<Outcome | null>(null);
  const [inputPrice, setInputPrice] = useState(
    provider?.inputMicrosPerMtok === undefined ? "" : dollarsInput(provider.inputMicrosPerMtok),
  );
  const [outputPrice, setOutputPrice] = useState(
    provider?.outputMicrosPerMtok === undefined ? "" : dollarsInput(provider.outputMicrosPerMtok),
  );
  const [cap, setCap] = useState(provider?.monthlyTokenCap === undefined ? "" : provider.monthlyTokenCap.toString());

  const test = useMutation(ProviderService.method.testProvider);
  const save = useMutation(ProviderService.method.setProvider, {
    onSettled: () => queryClient.invalidateQueries({ queryKey: providerQueryKey }),
  });

  const typed = trimmed({ baseUrl, model, apiKey });
  const tested = outcome?.ok ? outcome.for : null;
  const readiness = saveReadiness(typed, tested, provider);
  const shown = outcome !== null && sameTyped(outcome.for, typed) ? outcome : null;

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

  function runTest() {
    const snapshot = typed;
    setOutcome(null);
    test.mutate(
      { baseUrl: snapshot.baseUrl, model: snapshot.model, apiKey: snapshot.apiKey },
      {
        onSuccess: (data) =>
          setOutcome(
            data.ok
              ? { for: snapshot, ok: true, model: data.model, latencyMs: Number(data.latencyMs) }
              : { for: snapshot, ok: false, text: failureText(data.failure) },
          ),
        onError: (error) =>
          setOutcome({ for: snapshot, ok: false, text: providerErrorText(error, "Couldn't run the test. Try again.") }),
      },
    );
  }

  return (
    <form
      aria-label="AI provider"
      className="flex max-w-3xl flex-col gap-5 p-[22px]"
      onSubmit={(event) => {
        event.preventDefault();
        if (readiness !== "ready" || problem || input === null || output === null || limit === null) {
          return;
        }
        save.mutate({
          baseUrl: typed.baseUrl,
          model: typed.model,
          apiKey: typed.apiKey,
          inputMicrosPerMtok: input,
          outputMicrosPerMtok: output,
          monthlyTokenCap: limit,
          version: provider?.version ?? 0,
        });
      }}
    >
      <div className="grid grid-cols-2 gap-4 max-[860px]:grid-cols-1">
        <TextField
          label="Base URL"
          type="url"
          value={baseUrl}
          placeholder="https://api.openai.com/v1"
          autoComplete="off"
          spellCheck={false}
          onChange={(event) => setBaseUrl(event.target.value)}
        />
        <TextField
          label="Model name"
          value={model}
          placeholder="gpt-4o-mini"
          autoComplete="off"
          spellCheck={false}
          onChange={(event) => setModel(event.target.value)}
        />
      </div>
      <div>
        <div className="flex items-end gap-2">
          <div className="min-w-0 grow">
            <TextField
              label="API key"
              type={showKey ? "text" : "password"}
              value={apiKey}
                  placeholder={provider ? `Keep the key ending ${provider.keyLast4}` : "sk-..."}
              autoComplete="off"
              spellCheck={false}
              onChange={(event) => setApiKey(event.target.value)}
            />
          </div>
          <Button type="button" aria-pressed={showKey} onClick={() => setShowKey((value) => !value)}>
            {showKey ? "Hide" : "Show"}
          </Button>
        </div>
        <p className="mt-1.5 text-muted-foreground">
          {provider
            ? "Leave empty to keep the stored key. A key is never shown again."
            : "Stored encrypted. It is never shown again, and only used for your own calls."}
        </p>
      </div>
      <fieldset className="flex flex-col gap-3 border border-dashed border-line2 p-3.5">
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
        <p className="text-muted-foreground">
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
        <p className="text-muted-foreground">AI calls stop once the month&apos;s tokens would pass this. Empty means no limit.</p>
      </fieldset>
      {test.isPending ? (
        <p role="status" data-testid="test-result">
          Testing {typed.baseUrl}…
        </p>
      ) : shown?.ok ? (
        <ResultLine tone="ok" testId="test-result">
          Connected. {shown.model} replied in {shown.latencyMs} ms.
        </ResultLine>
      ) : shown ? (
        <ResultLine tone="bad" testId="test-result">
          {shown.text}
        </ResultLine>
      ) : null}
      {problem ? <p role="alert">{problem}</p> : null}
      {save.isError ? <p role="alert">{providerErrorText(save.error, "Couldn't save the provider. Try again.")}</p> : null}
      {save.isSuccess ? <p role="status">Saved.</p> : null}
      <p className="text-muted-foreground" data-testid="save-hint">
        Test sends one short message to the model. {HINTS[readiness] || "Save stays off until the test passes."}
      </p>
      <div className="flex flex-wrap gap-2">
        <Button type="button" disabled={!canTest(typed) || test.isPending} onClick={runTest}>
          {test.isPending ? "Testing…" : "Test connection"}
        </Button>
        <Button type="submit" variant="primary" disabled={readiness !== "ready" || problem !== null || save.isPending}>
          Save
        </Button>
      </div>
    </form>
  );
}
