"use client";

import { ConnectError, createClient } from "@connectrpc/connect";
import { useQuery, useTransport } from "@connectrpc/connect-query";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useMemo, useRef, useState } from "react";

import { approveErrorMessage } from "@/components/drafts/approve-error";
import { ApprovePanel, CopyPanel, VersionList } from "@/components/drafts/draft-panels";
import { draftKey, draftsQueueKey } from "@/components/drafts/drafts-key";
import { draftChannelLabel, draftKindLabel, draftStateLabel } from "@/components/drafts/labels";
import { PageHeader } from "@/components/shell/page-header";
import { Button } from "@/components/ui/button";
import { TextAreaField, TextField } from "@/components/ui/field";
import { DraftChannel, DraftState, DraftsService } from "@/gen/shogun/api/v1/drafts_pb";
import { bodyDigest } from "@/lib/digest";

const POLL_MS = 2000;
// WAIT_MS is how long the page keeps looking for something that finishes by
// itself (a new version, a mail going out) before it stops asking.
const WAIT_MS = 90_000;

interface Edit {
  subject: string;
  body: string;
}

interface DraftDetailProps {
  id: string;
}

export function DraftDetail({ id }: DraftDetailProps) {
  const transport = useTransport();
  const client = useMemo(() => createClient(DraftsService, transport), [transport]);
  const queryClient = useQueryClient();
  const [viewing, setViewing] = useState<number | null>(null);
  const [edit, setEdit] = useState<Edit | null>(null);
  const [extraContext, setExtraContext] = useState("");
  // A regenerated version the page is waiting for, so it keeps asking until it
  // arrives or WAIT_MS passes. It is state because the screen says so too.
  const [waiting, setWaiting] = useState<{ after: number; until: number } | null>(null);
  // When the page first saw the email being sent, so polling for the outcome
  // stops after WAIT_MS. Only the polling callback reads it.
  const sendingSince = useRef<number | null>(null);

  const detail = useQuery(
    DraftsService.method.getDraft,
    { id },
    {
      refetchInterval: (query) => {
        const draft = query.state.data?.draft;
        if (!draft) {
          return false;
        }
        const now = Date.now();
        if (draft.state === DraftState.GENERATING) {
          return POLL_MS;
        }
        if (draft.state === DraftState.APPROVED && draft.channel === DraftChannel.EMAIL) {
          sendingSince.current ??= now;
          return now - sendingSince.current < WAIT_MS ? POLL_MS : false;
        }
        if (waiting && draft.currentVersion <= waiting.after && now < waiting.until) {
          return POLL_MS;
        }
        return false;
      },
    },
  );

  async function refresh() {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: draftKey }),
      queryClient.invalidateQueries({ queryKey: draftsQueueKey }),
    ]);
  }

  const save = useMutation({
    mutationFn: (input: { subject: string; body: string; version: number }) =>
      client.editDraft({ id, ...input }),
    onSuccess: async () => {
      await refresh();
      setEdit(null);
      setViewing(null);
    },
  });
  const regenerate = useMutation({
    mutationFn: (input: { extraContext: string; version: number; after: number }) =>
      client.regenerate({ id, extraContext: input.extraContext, version: input.version }),
    onSuccess: async (_, input) => {
      setWaiting({ after: input.after, until: Date.now() + WAIT_MS });
      // Stop saying a version is on its way if none ever arrives.
      window.setTimeout(() => setWaiting(null), WAIT_MS);
      await refresh();
      setExtraContext("");
    },
  });
  const discard = useMutation({
    mutationFn: (version: number) => client.discard({ id, version }),
    onSuccess: refresh,
  });
  const markPosted = useMutation({
    mutationFn: (version: number) => client.markPosted({ id, version }),
    // After a refusal the draft may have moved, so show what it is now.
    onSettled: refresh,
  });
  const approve = useMutation({
    mutationFn: async (input: { version: number; subject: string; body: string }) =>
      client.approve({
        id,
        version: input.version,
        bodySha256: await bodyDigest(input.subject, input.body),
      }),
    onSuccess: async () => {
      sendingSince.current = null;
      await refresh();
    },
    // After any refusal the draft may have moved (back to the queue, or changed
    // under us), so show what it is now.
    onError: refresh,
  });

  if (detail.isPending) {
    return <div aria-busy="true" className="min-h-40 border-b border-ink p-[22px]" />;
  }
  if (detail.isError || !detail.data.draft) {
    return (
      <div className="flex items-center gap-3 border-b border-ink p-[22px]">
        <p role="alert">Couldn&apos;t load this draft.</p>
        <Button onClick={() => detail.refetch()}>Retry</Button>
      </div>
    );
  }

  const draft = detail.data.draft;
  const versions = detail.data.versions;
  const current = versions.find((version) => version.version === draft.currentVersion);
  const viewed = versions.find((version) => version.version === (viewing ?? draft.currentVersion)) ?? current;
  const isEmail = draft.channel === DraftChannel.EMAIL;
  const editable = draft.state === DraftState.PENDING || draft.state === DraftState.APPROVED;
  const text: Edit = edit ?? { subject: viewed?.subject ?? "", body: viewed?.body ?? "" };
  const dirty = edit !== null && (edit.subject !== (viewed?.subject ?? "") || edit.body !== (viewed?.body ?? ""));

  let blocked = "";
  if (dirty) {
    blocked = "You have an unsaved edit. Save it as a new version, then approve that.";
  } else if (viewed && current && viewed.version !== current.version) {
    blocked = `You are looking at version ${viewed.version}. Only the current version, ${current.version}, can be approved.`;
  }

  return (
    <>
      <PageHeader title="Review." description={headerText(draft.state)}>
        <Link href="/drafts" className="label inline-flex min-h-9 items-center border border-ink px-3 font-bold hover:bg-ink hover:text-paper">
          Back to drafts
        </Link>
      </PageHeader>

      <div className="label flex flex-wrap items-center gap-x-6 gap-y-2 border-b border-ink bg-strip px-[22px] py-3">
        <span>{draftKindLabel(draft.kind)}</span>
        <span>{draftChannelLabel(draft.channel)}</span>
        {draft.recipient ? <span>To {draft.recipient}</span> : null}
        <span data-testid="draft-state" className="border border-ink bg-paper px-2 py-0.5">
          {draftStateLabel(draft.state)}
        </span>
      </div>

      {draft.state === DraftState.FAILED ? (
        <p role="alert" className="border-b border-ink p-[22px]">
          This draft could not be written{draft.failureReason ? ` (${draft.failureReason})` : ""}. Nothing was sent.
        </p>
      ) : null}
      {draft.state === DraftState.GENERATING ? (
        <p role="status" className="border-b border-ink p-[22px] text-muted-ink">
          Writing the first version. This page updates by itself.
        </p>
      ) : null}
      {draft.state === DraftState.APPROVED && isEmail ? (
        <p role="status" className="border-b border-ink p-[22px] text-muted-ink">
          Approved. The email is being sent. This page updates by itself.
        </p>
      ) : null}
      {draft.state === DraftState.SENT ? (
        <p role="status" className="border-b border-ink p-[22px]">
          Sent.
        </p>
      ) : null}

      {versions.length > 0 && viewed ? (
        <VersionList
          versions={versions}
          currentVersion={draft.currentVersion}
          viewing={viewed.version}
          onSelect={(version) => {
            setViewing(version);
            setEdit(null);
          }}
        />
      ) : null}

      {viewed ? (
        <section aria-labelledby="text-heading" className="grid gap-4 border-b border-ink p-[22px]">
          <h2 id="text-heading" className="label font-bold">
            {editable ? "Edit" : "Text"}
          </h2>
          {isEmail ? (
            <TextField
              label="Subject"
              value={text.subject}
              readOnly={!editable}
              onChange={(event) => setEdit({ ...text, subject: event.target.value })}
            />
          ) : null}
          <TextAreaField
            label="Body"
            className="min-h-56"
            value={text.body}
            readOnly={!editable}
            onChange={(event) => setEdit({ ...text, body: event.target.value })}
          />
          {save.isError ? (
            <p role="alert" className="border border-ink bg-strip px-3 py-2">
              {ConnectError.from(save.error).rawMessage}
            </p>
          ) : null}
          {editable ? (
            <div className="flex flex-wrap items-center gap-3">
              <Button
                disabled={!dirty || save.isPending}
                onClick={() => save.mutate({ subject: text.subject, body: text.body, version: draft.version })}
              >
                Save as new version
              </Button>
              {dirty ? <span className="text-muted-ink">Not saved yet.</span> : null}
            </div>
          ) : null}
        </section>
      ) : null}

      {draft.state === DraftState.PENDING && viewed ? (
        <section aria-labelledby="regenerate-heading" className="grid gap-3 border-b border-ink p-[22px]">
          <h2 id="regenerate-heading" className="label font-bold">
            Write it again
          </h2>
          <TextAreaField
            label="Anything to change or keep in mind?"
            value={extraContext}
            onChange={(event) => setExtraContext(event.target.value)}
          />
          {regenerate.isError ? (
            <p role="alert" className="border border-ink bg-strip px-3 py-2">
              {ConnectError.from(regenerate.error).rawMessage}
            </p>
          ) : null}
          <div>
            <Button
              disabled={regenerate.isPending}
              onClick={() =>
                regenerate.mutate({ extraContext: extraContext.trim(), version: draft.version, after: draft.currentVersion })
              }
            >
              Regenerate
            </Button>
          </div>
          {waiting && draft.currentVersion <= waiting.after ? (
            <p role="status" className="text-muted-ink">
              Writing a new version. It will appear above.
            </p>
          ) : null}
        </section>
      ) : null}

      {draft.state === DraftState.PENDING && current ? (
        <ApprovePanel
          draft={draft}
          version={current}
          blocked={blocked}
          pending={approve.isPending}
          error={approve.isError ? approveErrorMessage(approve.error) : ""}
          onApprove={() =>
            approve.mutate({ version: current.version, subject: current.subject, body: current.body })
          }
        />
      ) : null}

      {draft.state === DraftState.APPROVED && !isEmail && current ? (
        <CopyPanel
          channel={draft.channel}
          version={current}
          onMarkPosted={() => markPosted.mutate(draft.version)}
          markingPosted={markPosted.isPending}
          markPostedError={markPosted.isError ? ConnectError.from(markPosted.error).rawMessage : undefined}
        />
      ) : null}

      {draft.state === DraftState.PENDING ? (
        <section className="border-b border-ink p-[22px]">
          {discard.isError ? (
            <p role="alert" className="mb-3 border border-ink bg-strip px-3 py-2">
              {ConnectError.from(discard.error).rawMessage}
            </p>
          ) : null}
          <Button disabled={discard.isPending} onClick={() => discard.mutate(draft.version)}>
            Discard draft
          </Button>
        </section>
      ) : null}
    </>
  );
}

function headerText(state: DraftState): string {
  switch (state) {
    case DraftState.PENDING:
      return "Read it, change it if you like, then approve. Nothing is sent until you do.";
    case DraftState.APPROVED:
      return "You approved this version.";
    case DraftState.SENT:
      return "This draft went out.";
    case DraftState.GENERATING:
      return "Shogun is writing this draft.";
    case DraftState.DISCARDED:
      return "You discarded this draft.";
    default:
      return "This draft needs another look.";
  }
}
