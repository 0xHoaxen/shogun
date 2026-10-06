"use client";

import { ConnectError, createClient } from "@connectrpc/connect";
import { useTransport } from "@connectrpc/connect-query";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { useMemo, useState, type FormEvent } from "react";

import { draftsQueueKey } from "@/components/drafts/drafts-key";
import { draftChannelLabel, draftKindLabel } from "@/components/drafts/labels";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { SelectField, TextAreaField, TextField } from "@/components/ui/field";
import {
  DraftChannel,
  DraftKind,
  DraftTarget,
  DraftsService,
} from "@/gen/shogun/api/v1/drafts_pb";

const IDEMPOTENCY_KEY_HEADER = "Idempotency-Key";

// A draft started by hand is about nothing in particular. Cover letters and
// outreach are drafted by Shogun itself when a job or contact is added or moves.
const KINDS = [DraftKind.POST, DraftKind.ONE_OFF] as const;
const CHANNELS = [DraftChannel.EMAIL, DraftChannel.LINKEDIN, DraftChannel.X, DraftChannel.OTHER] as const;

interface AddDraftDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function AddDraftDialog({ open, onOpenChange }: AddDraftDialogProps) {
  const transport = useTransport();
  const client = useMemo(() => createClient(DraftsService, transport), [transport]);
  const queryClient = useQueryClient();
  const router = useRouter();
  const [kind, setKind] = useState<DraftKind>(DraftKind.POST);
  const [channel, setChannel] = useState<DraftChannel>(DraftChannel.LINKEDIN);
  const [recipient, setRecipient] = useState("");
  const [extraContext, setExtraContext] = useState("");
  // One key per intended draft: a retry after a failure returns the original.
  const [idempotencyKey, setIdempotencyKey] = useState(() => crypto.randomUUID());

  const generate = useMutation({
    mutationFn: () =>
      client.generateDraft(
        {
          kind,
          target: DraftTarget.NONE,
          channel,
          recipient: channel === DraftChannel.EMAIL ? recipient.trim() : "",
          extraContext: extraContext.trim(),
        },
        { headers: { [IDEMPOTENCY_KEY_HEADER]: idempotencyKey } },
      ),
    onSuccess: async (response) => {
      await queryClient.invalidateQueries({ queryKey: draftsQueueKey });
      setExtraContext("");
      setRecipient("");
      setIdempotencyKey(crypto.randomUUID());
      generate.reset();
      onOpenChange(false);
      if (response.draft) {
        router.push(`/drafts/${response.draft.id}`);
      }
    },
  });

  const needsRecipient = channel === DraftChannel.EMAIL;
  const canSubmit =
    extraContext.trim() !== "" && (!needsRecipient || recipient.trim() !== "") && !generate.isPending;

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (canSubmit) {
      generate.mutate();
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="New draft"
      description="Say what it should be about. Shogun writes it in your voice, and you approve it before anything leaves."
    >
      <form onSubmit={handleSubmit} className="grid gap-4 p-[22px]">
        <SelectField label="Kind" value={kind} onChange={(event) => setKind(Number(event.target.value) as DraftKind)}>
          {KINDS.map((option) => (
            <option key={option} value={option}>
              {draftKindLabel(option)}
            </option>
          ))}
        </SelectField>
        <SelectField
          label="Channel"
          value={channel}
          onChange={(event) => setChannel(Number(event.target.value) as DraftChannel)}
        >
          {CHANNELS.map((option) => (
            <option key={option} value={option}>
              {draftChannelLabel(option)}
            </option>
          ))}
        </SelectField>
        {needsRecipient ? (
          <TextField
            label="To"
            type="email"
            required
            value={recipient}
            onChange={(event) => setRecipient(event.target.value)}
          />
        ) : null}
        <TextAreaField
          label="What should it say?"
          required
          value={extraContext}
          onChange={(event) => setExtraContext(event.target.value)}
        />
        {generate.isError ? (
          <p role="alert" className="border border-ink bg-strip px-3 py-2">
            {ConnectError.from(generate.error).rawMessage}
          </p>
        ) : null}
        <div className="flex justify-end gap-2">
          <Button type="button" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={!canSubmit}>
            Write draft
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
