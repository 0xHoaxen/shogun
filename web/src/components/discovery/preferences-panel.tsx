"use client";

import { useQuery } from "@connectrpc/connect-query";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";

import { postingsKey, preferencesKey } from "@/components/discovery/discovery-key";
import { discoveryErrorText } from "@/components/discovery/errors";
import { listFromText, percent, textFromList } from "@/components/discovery/labels";
import { useDiscoveryClient } from "@/components/discovery/use-discovery-client";
import { Button } from "@/components/ui/button";
import { TextField } from "@/components/ui/field";
import { DiscoveryService, type DiscoveryPreferences } from "@/gen/shogun/api/v1/discovery_pb";

export function PreferencesPanel() {
  const loaded = useQuery(DiscoveryService.method.getDiscoveryPreferences, {});
  if (loaded.isError) {
    return (
      <div className="flex items-center gap-3 border-b border-ink p-[22px]">
        <p role="alert">Couldn&apos;t load your preferences.</p>
        <Button onClick={() => loaded.refetch()}>Retry</Button>
      </div>
    );
  }
  if (!loaded.data?.preferences) {
    return <div aria-busy="true" className="border-b border-ink p-[22px]" />;
  }
  return <PreferencesForm saved={loaded.data.preferences} />;
}

const LISTS = [
  ["roles", "Roles you want (matched in the title)"],
  ["locations", "Places you want"],
  ["mustHave", "Terms a posting must mention"],
  ["niceToHave", "Terms that are a plus"],
  ["exclude", "Terms that rule a posting out"],
] as const;

type ListKey = (typeof LISTS)[number][0];

function PreferencesForm({ saved }: { saved: DiscoveryPreferences }) {
  const client = useDiscoveryClient();
  const queryClient = useQueryClient();
  const [text, setText] = useState<Record<ListKey, string>>({
    roles: textFromList(saved.roles),
    locations: textFromList(saved.locations),
    mustHave: textFromList(saved.mustHave),
    niceToHave: textFromList(saved.niceToHave),
    exclude: textFromList(saved.exclude),
  });
  const [minScore, setMinScore] = useState(String(Math.round(saved.minScore * 100)));

  const save = useMutation({
    mutationFn: async () => {
      await client.setDiscoveryPreferences({
        preferences: {
          roles: listFromText(text.roles),
          locations: listFromText(text.locations),
          mustHave: listFromText(text.mustHave),
          niceToHave: listFromText(text.niceToHave),
          exclude: listFromText(text.exclude),
          minScore: Number(minScore) / 100,
        },
      });
    },
    onSuccess: async () => {
      // Saving scores every posting again, so both lists are stale.
      await queryClient.invalidateQueries({ queryKey: preferencesKey });
      await queryClient.invalidateQueries({ queryKey: postingsKey });
    },
  });

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!save.isPending) {
      save.mutate();
    }
  }

  return (
    <form onSubmit={handleSubmit} className="grid gap-4 border-b border-ink p-[22px]">
      <p className="text-muted-ink">
        Separate terms with commas. Saving scores your postings again. Nothing matches until you set at least one.
      </p>
      {LISTS.map(([key, label]) => (
        <TextField key={key} label={label} value={text[key]} onChange={(event) => setText({ ...text, [key]: event.target.value })} />
      ))}
      <TextField
        label={`Count a posting as a match at (${percent(Number(minScore) / 100 || 0)})`}
        type="number"
        min={0}
        max={100}
        value={minScore}
        onChange={(event) => setMinScore(event.target.value)}
      />
      {save.isError ? <p role="alert">{discoveryErrorText(save.error, "Couldn't save your preferences. Try again.")}</p> : null}
      {save.isSuccess ? <p role="status">Saved. Your postings are being scored again.</p> : null}
      <div>
        <Button type="submit" variant="primary" disabled={save.isPending}>
          Save preferences
        </Button>
      </div>
    </form>
  );
}
