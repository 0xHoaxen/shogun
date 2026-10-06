"use client";

import { useInfiniteQuery } from "@connectrpc/connect-query";
import Link from "next/link";
import { useState } from "react";

import { AddDraftDialog } from "@/components/drafts/add-draft-dialog";
import {
  QUEUE_STATES,
  draftChannelLabel,
  draftKindLabel,
  draftStateLabel,
} from "@/components/drafts/labels";
import { PageHeader } from "@/components/shell/page-header";
import { Button } from "@/components/ui/button";
import { DraftState, DraftsService } from "@/gen/shogun/api/v1/drafts_pb";
import { cn } from "@/lib/utils";

const PAGE_SIZE = 30;
const HEADINGS = ["Draft", "Kind", "Channel", "To", "State", ""] as const;
// REFRESH_MS is how often states that change by themselves are refetched.
const REFRESH_MS = 4000;

export function DraftsQueue() {
  const [state, setState] = useState<DraftState>(DraftState.PENDING);
  const [adding, setAdding] = useState(false);

  const drafts = useInfiniteQuery(
    DraftsService.method.listQueue,
    { state, pageSize: PAGE_SIZE, pageToken: "" },
    {
      pageParamKey: "pageToken",
      getNextPageParam: (last) => last.nextPageToken || undefined,
      // A draft being written, or an email being sent, finishes without anyone
      // touching the page.
      refetchInterval:
        state === DraftState.GENERATING || state === DraftState.APPROVED ? REFRESH_MS : false,
    },
  );
  const rows = drafts.data?.pages.flatMap((page) => page.drafts) ?? [];

  return (
    <>
      <PageHeader
        title="Your drafts."
        description="Everything Shogun wrote for you. Nothing leaves until you approve the exact text."
      >
        <Button variant="primary" onClick={() => setAdding(true)}>
          New draft
        </Button>
      </PageHeader>

      <div role="group" aria-label="Filter by state" className="flex flex-wrap border-b border-ink p-[22px] pb-[23px]">
        {QUEUE_STATES.map((filter) => (
          <button
            key={filter}
            type="button"
            aria-pressed={state === filter}
            onClick={() => setState(filter)}
            className={cn(
              "label -mr-px -mb-px inline-flex min-h-9 cursor-pointer items-center border border-ink px-3 font-medium hover:bg-strip",
              state === filter && "bg-ink text-paper hover:bg-ink",
            )}
          >
            {draftStateLabel(filter)}
          </button>
        ))}
      </div>

      {drafts.isError ? (
        <div className="flex items-center gap-3 border-b border-ink p-[22px]">
          <p role="alert">Couldn&apos;t load your drafts.</p>
          <Button onClick={() => drafts.refetch()}>Retry</Button>
        </div>
      ) : (
        <div className="overflow-x-auto border-b border-ink">
          <table className="w-full min-w-[860px] border-collapse text-left">
            <thead>
              <tr className="label h-9 bg-ink text-paper">
                {HEADINGS.map((heading, index) => (
                  <th key={`${heading}-${index}`} className="px-2.5 font-medium">
                    {heading}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((draft) => (
                <tr key={draft.id} className="border-b border-ink align-top">
                  <td className="max-w-[360px] px-2.5 py-2.5">
                    <b>{draft.subject || "(no subject)"}</b>
                    <br />
                    <span className="text-[11px] text-muted-ink">{draft.preview}</span>
                  </td>
                  <td className="px-2.5 py-2.5">{draftKindLabel(draft.kind)}</td>
                  <td className="px-2.5 py-2.5">{draftChannelLabel(draft.channel)}</td>
                  <td className="px-2.5 py-2.5">{draft.recipient || "-"}</td>
                  <td className="px-2.5 py-2.5">
                    <span className="label inline-flex min-h-5 items-center border border-ink px-2 text-[11px]">
                      {draftStateLabel(draft.state)}
                    </span>
                  </td>
                  <td className="px-2.5 py-2.5 text-right">
                    <Link
                      href={`/drafts/${draft.id}`}
                      className="label inline-flex min-h-9 items-center border border-ink px-3 font-bold hover:bg-ink hover:text-paper"
                    >
                      {draft.state === DraftState.PENDING ? "Review" : "Open"}
                    </Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {drafts.isSuccess && rows.length === 0 ? (
            <p className="p-[22px] text-muted-ink">Nothing here.</p>
          ) : null}
        </div>
      )}

      {drafts.hasNextPage ? (
        <div className="border-b border-ink p-[22px]">
          <Button disabled={drafts.isFetchingNextPage} onClick={() => drafts.fetchNextPage()}>
            Load more
          </Button>
        </div>
      ) : null}

      <AddDraftDialog open={adding} onOpenChange={setAdding} />
    </>
  );
}
