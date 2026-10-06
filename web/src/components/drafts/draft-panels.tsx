"use client";

import { useState } from "react";

import { draftChannelLabel } from "@/components/drafts/labels";
import { Button } from "@/components/ui/button";
import {
  DraftChannel,
  VersionAuthor,
  type Draft,
  type DraftVersion,
} from "@/gen/shogun/api/v1/drafts_pb";
import { cn } from "@/lib/utils";

interface VersionListProps {
  versions: readonly DraftVersion[];
  currentVersion: number;
  viewing: number;
  onSelect: (version: number) => void;
}

// The versions of a draft, newest first. Selecting one shows its text in the
// editor; only the current one can be approved.
export function VersionList({ versions, currentVersion, viewing, onSelect }: VersionListProps) {
  return (
    <div role="group" aria-label="Versions" className="flex flex-wrap border-b border-ink p-[22px] pb-[23px]">
      {versions.map((version) => (
        <button
          key={version.version}
          type="button"
          aria-pressed={viewing === version.version}
          onClick={() => onSelect(version.version)}
          className={cn(
            "label -mr-px -mb-px inline-flex min-h-9 cursor-pointer items-center gap-2 border border-ink px-3 font-medium hover:bg-strip",
            viewing === version.version && "bg-ink text-paper hover:bg-ink",
          )}
        >
          {`v${version.version}`}
          <span className="text-[10px] opacity-80">
            {version.author === VersionAuthor.USER ? "you" : "ai"}
            {version.version === currentVersion ? " · current" : ""}
          </span>
        </button>
      ))}
    </div>
  );
}

interface ApprovePanelProps {
  draft: Draft;
  version: DraftVersion;
  // blocked is why approving is not possible right now, or "" when it is.
  blocked: string;
  pending: boolean;
  error: string;
  onApprove: () => void;
}

// ApprovePanel shows exactly what approving will let out, from the stored
// version, never from the editor: the owner approves what they read here.
export function ApprovePanel({ draft, version, blocked, pending, error, onApprove }: ApprovePanelProps) {
  const isEmail = draft.channel === DraftChannel.EMAIL;
  return (
    <section aria-labelledby="approve-heading" className="border-b border-ink p-[22px]">
      <h2 id="approve-heading" className="label mb-3 font-bold">
        {isEmail ? "This email will be sent" : "Approve this text"}
      </h2>
      <div data-testid="send-preview" className="grid gap-2 border border-ink bg-field p-3.5 text-xs">
        <div>
          <span className="label text-[11px]">Channel </span>
          {draftChannelLabel(draft.channel)}
        </div>
        {isEmail ? (
          <div>
            <span className="label text-[11px]">To </span>
            {draft.recipient || "(no recipient)"}
          </div>
        ) : null}
        {isEmail ? (
          <div>
            <span className="label text-[11px]">Subject </span>
            {version.subject || "(no subject)"}
          </div>
        ) : null}
        <pre className="font-[inherit] leading-relaxed whitespace-pre-wrap">{version.body}</pre>
      </div>
      {blocked ? <p className="mt-3 text-muted-ink">{blocked}</p> : null}
      {error ? (
        <p role="alert" className="mt-3 border border-ink bg-strip px-3 py-2">
          {error}
        </p>
      ) : null}
      <div className="mt-3.5">
        <Button variant="primary" disabled={blocked !== "" || pending} onClick={onApprove}>
          {isEmail ? "Approve and send" : "Approve"}
        </Button>
      </div>
    </section>
  );
}

interface CopyPanelProps {
  channel: DraftChannel;
  version: DraftVersion;
}

// CopyPanel is what an approved LinkedIn, X or copy-only draft shows: Shogun
// never posts for the owner, so they copy the text and post it themselves.
export function CopyPanel({ channel, version }: CopyPanelProps) {
  const [copied, setCopied] = useState(false);
  const [failed, setFailed] = useState(false);

  async function copy() {
    try {
      await navigator.clipboard.writeText(version.body);
      setCopied(true);
      setFailed(false);
    } catch {
      setCopied(false);
      setFailed(true);
    }
  }

  return (
    <section aria-labelledby="copy-heading" className="border-b border-ink p-[22px]">
      <h2 id="copy-heading" className="label mb-3 font-bold">
        Approved. Copy it and post it yourself.
      </h2>
      <p className="mb-3 text-muted-ink">
        Shogun does not post to {draftChannelLabel(channel)} for you. Nothing has left yet.
      </p>
      <Button variant="primary" onClick={copy}>
        {copied ? "Copied" : "Copy text"}
      </Button>
      {failed ? (
        <p role="alert" className="mt-3 border border-ink bg-strip px-3 py-2">
          Your browser blocked copying. Select the text above and copy it by hand.
        </p>
      ) : null}
    </section>
  );
}
