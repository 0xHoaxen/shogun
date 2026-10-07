"use client";

import { useInfiniteQuery } from "@connectrpc/connect-query";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";

import { postingsKey } from "@/components/discovery/discovery-key";
import { discoveryErrorText } from "@/components/discovery/errors";
import { SCORE_FILTERS, percent } from "@/components/discovery/labels";
import { useDiscoveryClient } from "@/components/discovery/use-discovery-client";
import { Button } from "@/components/ui/button";
import { DiscoveryService, type DiscoveryPosting } from "@/gen/shogun/api/v1/discovery_pb";
import { cn } from "@/lib/utils";

const PAGE_SIZE = 20;
const WEB_LINK = /^https?:\/\//;

export function PostingsPanel() {
  const client = useDiscoveryClient();
  const queryClient = useQueryClient();
  const [minScore, setMinScore] = useState(0);

  const postings = useInfiniteQuery(
    DiscoveryService.method.listDiscoveryPostings,
    { minScore, sourceId: "", pageSize: PAGE_SIZE, pageToken: "" },
    { pageParamKey: "pageToken", getNextPageParam: (last) => last.nextPageToken || undefined },
  );
  const rows = postings.data?.pages.flatMap((page) => page.postings) ?? [];

  const save = useMutation({
    mutationFn: async (postingId: string) => {
      await client.saveDiscoveryPosting({ postingId });
    },
    // Whatever the outcome, show what shinobi holds now.
    onSettled: () => queryClient.invalidateQueries({ queryKey: postingsKey }),
  });

  return (
    <>
      <div role="group" aria-label="Lowest score" className="flex flex-wrap border-b border-ink p-[22px] pb-[23px]">
        {SCORE_FILTERS.map((filter) => (
          <button
            key={filter.value}
            type="button"
            aria-pressed={minScore === filter.value}
            onClick={() => setMinScore(filter.value)}
            className={cn(
              "label -mr-px -mb-px inline-flex min-h-9 cursor-pointer items-center border border-ink px-3 font-medium hover:bg-strip",
              minScore === filter.value && "bg-ink text-paper hover:bg-ink",
            )}
          >
            {filter.label}
          </button>
        ))}
      </div>

      {save.isError ? (
        <p role="alert" className="border-b border-ink p-[22px]">
          {discoveryErrorText(save.error, "Couldn't save the posting. Try again.")}
        </p>
      ) : null}

      {postings.isError ? (
        <div className="flex items-center gap-3 border-b border-ink p-[22px]">
          <p role="alert">Couldn&apos;t load your postings.</p>
          <Button onClick={() => postings.refetch()}>Retry</Button>
        </div>
      ) : (
        <>
          {rows.map((posting) => (
            <PostingCard
              key={posting.id}
              posting={posting}
              busy={save.isPending && save.variables === posting.id}
              onSave={(id) => save.mutate(id)}
            />
          ))}
          {postings.isSuccess && rows.length === 0 ? (
            <p className="border-b border-ink p-[22px] text-muted-ink">
              Nothing here yet. Add a source and set what you are looking for.
            </p>
          ) : null}
          {postings.hasNextPage ? (
            <div className="border-b border-ink p-[22px]">
              <Button disabled={postings.isFetchingNextPage} onClick={() => postings.fetchNextPage()}>
                Load more
              </Button>
            </div>
          ) : null}
        </>
      )}
    </>
  );
}

interface PostingCardProps {
  posting: DiscoveryPosting;
  busy: boolean;
  onSave: (id: string) => void;
}

function PostingCard({ posting, busy, onSave }: PostingCardProps) {
  const where = [posting.company, posting.location].filter(Boolean).join(" · ");
  return (
    <article aria-label={posting.title} className="grid gap-2 border-b border-ink p-[22px]">
      <div className="flex flex-wrap items-center gap-2">
        <b>{posting.title}</b>
        {posting.scored ? (
          <span className="label inline-flex min-h-5 items-center border border-ink px-2 text-[11px]">
            {percent(posting.score)}
          </span>
        ) : (
          <span className="label inline-flex min-h-5 items-center border border-dashed border-ink px-2 text-[11px]">
            Not scored yet
          </span>
        )}
      </div>
      {where ? <p className="text-muted-ink">{where}</p> : null}
      {posting.reasons.length > 0 ? (
        <ul aria-label="Why" className="list-disc pl-5 text-[11px] text-muted-ink">
          {posting.reasons.map((reason) => (
            <li key={reason}>{reason}</li>
          ))}
        </ul>
      ) : null}
      <div className="flex flex-wrap items-center gap-3">
        {WEB_LINK.test(posting.url) ? (
          <a href={posting.url} target="_blank" rel="noopener noreferrer" className="underline">
            Open posting
          </a>
        ) : null}
        {posting.savedJobId ? (
          <Link href="/jobs" className="label inline-flex min-h-9 items-center border border-ink px-3 font-bold">
            Saved to the board
          </Link>
        ) : (
          <Button disabled={busy} aria-label={`Save ${posting.title} to the tracker`} onClick={() => onSave(posting.id)}>
            Save to tracker
          </Button>
        )}
      </div>
    </article>
  );
}
