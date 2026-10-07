"use client";

import { timestampDate } from "@bufbuild/protobuf/wkt";
import { useQuery } from "@connectrpc/connect-query";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { postingsKey, sourcesKey } from "@/components/discovery/discovery-key";
import { discoveryErrorText, lastErrorText } from "@/components/discovery/errors";
import { sourceKindLabel } from "@/components/discovery/labels";
import { SourceDialog } from "@/components/discovery/source-dialog";
import { useDiscoveryClient } from "@/components/discovery/use-discovery-client";
import { Button } from "@/components/ui/button";
import { DiscoveryService, type DiscoverySource } from "@/gen/shogun/api/v1/discovery_pb";

export function SourcesPanel() {
  const client = useDiscoveryClient();
  const queryClient = useQueryClient();
  const sources = useQuery(DiscoveryService.method.listDiscoverySources, {});
  const [editing, setEditing] = useState<DiscoverySource | null>(null);
  const [adding, setAdding] = useState(false);
  const [note, setNote] = useState("");

  const run = useMutation({
    mutationFn: (id: string) => client.runDiscoverySource({ id }),
    onMutate: () => setNote(""),
    onSuccess: (response) => setNote(`Read ${response.fetched} postings, ${response.added} of them new.`),
    // A failed run is kept with the source, so show the source as it is now.
    onSettled: async () => {
      await queryClient.invalidateQueries({ queryKey: sourcesKey });
      await queryClient.invalidateQueries({ queryKey: postingsKey });
    },
  });

  const toggle = useMutation({
    mutationFn: async (source: DiscoverySource) => {
      await client.saveDiscoverySource({ source: { ...source, enabled: !source.enabled } });
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: sourcesKey }),
  });

  const rows = sources.data?.sources ?? [];
  return (
    <>
      <div className="flex items-center justify-between gap-3 border-b border-ink p-[22px]">
        <p className="text-muted-ink">Shogun reads only these. Nothing else is searched.</p>
        <Button variant="primary" onClick={() => setAdding(true)}>
          Add source
        </Button>
      </div>

      {note ? (
        <p role="status" className="border-b border-ink p-[22px]">
          {note}
        </p>
      ) : null}
      {run.isError ? (
        <p role="alert" className="border-b border-ink p-[22px]">
          {discoveryErrorText(run.error, "Couldn't read the source. Try again.")}
        </p>
      ) : null}
      {toggle.isError ? (
        <p role="alert" className="border-b border-ink p-[22px]">
          {discoveryErrorText(toggle.error, "Couldn't change the source. Try again.")}
        </p>
      ) : null}

      {sources.isError ? (
        <div className="flex items-center gap-3 border-b border-ink p-[22px]">
          <p role="alert">Couldn&apos;t load your sources.</p>
          <Button onClick={() => sources.refetch()}>Retry</Button>
        </div>
      ) : (
        <>
          {rows.map((source) => (
            <article key={source.id} aria-label={source.name} className="grid gap-2 border-b border-ink p-[22px]">
              <div className="flex flex-wrap items-center gap-2">
                <b>{source.name}</b>
                <span className="label inline-flex min-h-5 items-center border border-ink px-2 text-[11px]">
                  {sourceKindLabel(source.kind)}
                </span>
                {source.enabled ? null : (
                  <span className="label inline-flex min-h-5 items-center border border-dashed border-ink px-2 text-[11px]">
                    Off
                  </span>
                )}
              </div>
              <p className="text-[11px] text-muted-ink">
                {source.config?.url || "Pasted JSON"} · {source.schedule}
                {source.lastRunAt ? ` · last read ${timestampDate(source.lastRunAt).toLocaleString()}` : " · never read"}
              </p>
              {source.lastError ? (
                <p className="text-[11px]">Last read failed: {lastErrorText(source.lastError)}.</p>
              ) : null}
              <div className="flex flex-wrap gap-2">
                <Button
                  disabled={run.isPending && run.variables === source.id}
                  aria-label={`Read ${source.name} now`}
                  onClick={() => run.mutate(source.id)}
                >
                  Read now
                </Button>
                <Button aria-label={`Edit ${source.name}`} onClick={() => setEditing(source)}>
                  Edit
                </Button>
                <Button
                  disabled={toggle.isPending}
                  aria-label={`${source.enabled ? "Turn off" : "Turn on"} ${source.name}`}
                  onClick={() => toggle.mutate(source)}
                >
                  {source.enabled ? "Turn off" : "Turn on"}
                </Button>
              </div>
            </article>
          ))}
          {sources.isSuccess && rows.length === 0 ? (
            <p className="border-b border-ink p-[22px] text-muted-ink">No sources yet.</p>
          ) : null}
        </>
      )}

      {adding ? <SourceDialog key="add" open onOpenChange={setAdding} /> : null}
      {editing ? (
        <SourceDialog key={editing.id} open source={editing} onOpenChange={(open) => !open && setEditing(null)} />
      ) : null}
    </>
  );
}
