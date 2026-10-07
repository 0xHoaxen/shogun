"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";

import { sourcesKey } from "@/components/discovery/discovery-key";
import { discoveryErrorText } from "@/components/discovery/errors";
import { SOURCE_KINDS, sourceKindLabel } from "@/components/discovery/labels";
import { useDiscoveryClient } from "@/components/discovery/use-discovery-client";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { SelectField, TextAreaField, TextField } from "@/components/ui/field";
import { DiscoverySourceKind, type DiscoverySource } from "@/gen/shogun/api/v1/discovery_pb";

const DEFAULT_SCHEDULE = "0 7 * * *";

interface SourceDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  // source is the one being edited; empty when adding.
  source?: DiscoverySource;
}

const MAPPING_FIELDS = [
  ["itemsPath", "Where the list is (blank if the document is the list)"],
  ["id", "Id field"],
  ["title", "Title field"],
  ["company", "Company field"],
  ["url", "Link field"],
  ["location", "Location field"],
  ["postedAt", "Date field"],
  ["description", "Description field"],
] as const;

type MappingKey = (typeof MAPPING_FIELDS)[number][0];

export function SourceDialog({ open, onOpenChange, source }: SourceDialogProps) {
  const client = useDiscoveryClient();
  const queryClient = useQueryClient();
  const editing = Boolean(source);
  const [name, setName] = useState(source?.name ?? "");
  const [kind, setKind] = useState<DiscoverySourceKind>(source?.kind ?? DiscoverySourceKind.RSS);
  const [url, setUrl] = useState(source?.config?.url ?? "");
  const [document, setDocument] = useState(source?.config?.document ?? "");
  const [schedule, setSchedule] = useState(source?.schedule ?? DEFAULT_SCHEDULE);
  const [enabled, setEnabled] = useState(source?.enabled ?? true);
  const [mapping, setMapping] = useState<Record<MappingKey, string>>({
    itemsPath: source?.config?.mapping?.itemsPath ?? "",
    id: source?.config?.mapping?.id ?? "",
    title: source?.config?.mapping?.title ?? "",
    company: source?.config?.mapping?.company ?? "",
    url: source?.config?.mapping?.url ?? "",
    location: source?.config?.mapping?.location ?? "",
    postedAt: source?.config?.mapping?.postedAt ?? "",
    description: source?.config?.mapping?.description ?? "",
  });

  const needsUrl = kind === DiscoverySourceKind.RSS || kind === DiscoverySourceKind.API;
  const needsMapping = kind === DiscoverySourceKind.API || kind === DiscoverySourceKind.FILE;

  const save = useMutation({
    mutationFn: async () => {
      await client.saveDiscoverySource({
        source: {
          id: source?.id ?? "",
          name: name.trim(),
          kind,
          schedule: schedule.trim(),
          enabled,
          config: {
            url: needsUrl ? url.trim() : "",
            document: kind === DiscoverySourceKind.FILE ? document : "",
            mapping: needsMapping ? mapping : undefined,
          },
        },
      });
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: sourcesKey });
      save.reset();
      onOpenChange(false);
    },
  });

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (name.trim() !== "" && !save.isPending) {
      save.mutate();
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={editing ? "Edit source" : "Add source"}
      description="Shogun reads only the sources you add, politely, and respects each site's robots.txt."
    >
      <form onSubmit={handleSubmit} className="grid gap-4 p-[22px]">
        <TextField label="Name" required value={name} onChange={(event) => setName(event.target.value)} />
        <SelectField
          label="Kind"
          value={kind}
          disabled={editing}
          onChange={(event) => setKind(Number(event.target.value) as DiscoverySourceKind)}
        >
          {SOURCE_KINDS.map((option) => (
            <option key={option} value={option}>
              {sourceKindLabel(option)}
            </option>
          ))}
        </SelectField>
        {needsUrl ? (
          <TextField
            label="Address (https)"
            type="url"
            required
            value={url}
            onChange={(event) => setUrl(event.target.value)}
          />
        ) : (
          <TextAreaField
            label="The JSON"
            required
            value={document}
            onChange={(event) => setDocument(event.target.value)}
          />
        )}
        {needsMapping ? (
          <fieldset className="grid gap-3 border border-ink p-3">
            <legend className="label px-1 text-[11px]">Where each field is, as dot-separated keys</legend>
            {MAPPING_FIELDS.map(([key, label]) => (
              <TextField
                key={key}
                label={label}
                value={mapping[key]}
                onChange={(event) => setMapping({ ...mapping, [key]: event.target.value })}
              />
            ))}
          </fieldset>
        ) : null}
        <TextField
          label="Schedule (cron, India time)"
          value={schedule}
          onChange={(event) => setSchedule(event.target.value)}
        />
        <label className="flex items-center gap-2 text-xs">
          <input type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} />
          Read it on its schedule
        </label>
        {save.isError ? <p role="alert">{discoveryErrorText(save.error, "Couldn't save the source. Try again.")}</p> : null}
        <div className="flex gap-2">
          <Button type="submit" variant="primary" disabled={name.trim() === "" || save.isPending}>
            {editing ? "Save source" : "Add source"}
          </Button>
          <Button type="button" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
