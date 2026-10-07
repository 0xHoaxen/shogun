"use client";

import { useInfiniteQuery } from "@connectrpc/connect-query";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { profileErrorText } from "@/components/profile/errors";
import { STATE_FILTERS, suggestionStateLabel } from "@/components/profile/labels";
import { suggestionsKey } from "@/components/profile/profile-key";
import { SuggestionCard } from "@/components/profile/suggestion-card";
import { useProfileClient } from "@/components/profile/use-profile-client";
import { PageHeader } from "@/components/shell/page-header";
import { Button } from "@/components/ui/button";
import { ProfileService, SuggestionState } from "@/gen/shogun/api/v1/profile_pb";
import { cn } from "@/lib/utils";

const PAGE_SIZE = 20;

type Decision = "accept" | "dismiss";

export function ProfileScreen() {
  const client = useProfileClient();
  const queryClient = useQueryClient();
  const [state, setState] = useState<SuggestionState>(SuggestionState.OPEN);
  const [syncNote, setSyncNote] = useState("");

  const suggestions = useInfiniteQuery(
    ProfileService.method.listProfileSuggestions,
    { state, pageSize: PAGE_SIZE, pageToken: "" },
    { pageParamKey: "pageToken", getNextPageParam: (last) => last.nextPageToken || undefined },
  );
  const rows = suggestions.data?.pages.flatMap((page) => page.suggestions) ?? [];

  const decide = useMutation({
    mutationFn: async ({ id, how }: { id: string; how: Decision }) => {
      // The refetch shows the result, so the answer itself is not needed.
      await (how === "accept" ? client.acceptProfileSuggestion({ id }) : client.dismissProfileSuggestion({ id }));
    },
    // Whatever the outcome, show what katana holds now.
    onSettled: () => queryClient.invalidateQueries({ queryKey: suggestionsKey }),
  });

  const sync = useMutation({
    mutationFn: () => client.syncGitHub({}),
    onMutate: () => setSyncNote(""),
    onSuccess: async (response) => {
      setSyncNote(
        response.changed
          ? "GitHub has new activity. New suggestions will appear here shortly."
          : "GitHub has nothing new since the last sync.",
      );
      await queryClient.invalidateQueries({ queryKey: suggestionsKey });
    },
  });

  return (
    <>
      <PageHeader
        title="Your profile."
        description="Edits to your resume and LinkedIn that Shogun found in your GitHub activity and what you finished learning. You make the edits; accepting only records your choice."
      >
        <Button variant="primary" disabled={sync.isPending} onClick={() => sync.mutate()}>
          Sync GitHub
        </Button>
      </PageHeader>

      {sync.isError ? (
        <p role="alert" className="border-b border-ink p-[22px]">
          {profileErrorText(sync.error, "Couldn't sync GitHub. Try again.")}
        </p>
      ) : null}
      {syncNote ? (
        <p role="status" className="border-b border-ink p-[22px]">
          {syncNote}
        </p>
      ) : null}

      <div role="group" aria-label="Filter by state" className="flex flex-wrap border-b border-ink p-[22px] pb-[23px]">
        {STATE_FILTERS.map((filter) => (
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
            {suggestionStateLabel(filter)}
          </button>
        ))}
      </div>

      {decide.isError ? (
        <p role="alert" className="border-b border-ink p-[22px]">
          {profileErrorText(decide.error, "Couldn't save your choice. Try again.")}
        </p>
      ) : null}

      {suggestions.isError ? (
        <div className="flex items-center gap-3 border-b border-ink p-[22px]">
          <p role="alert">Couldn&apos;t load your suggestions.</p>
          <Button onClick={() => suggestions.refetch()}>Retry</Button>
        </div>
      ) : (
        <>
          {rows.map((suggestion) => (
            <SuggestionCard
              key={suggestion.id}
              suggestion={suggestion}
              busy={decide.isPending && decide.variables.id === suggestion.id}
              onAccept={(id) => decide.mutate({ id, how: "accept" })}
              onDismiss={(id) => decide.mutate({ id, how: "dismiss" })}
            />
          ))}
          {suggestions.isSuccess && rows.length === 0 ? (
            <p className="border-b border-ink p-[22px] text-muted-ink">Nothing here.</p>
          ) : null}
          {suggestions.hasNextPage ? (
            <div className="border-b border-ink p-[22px]">
              <Button disabled={suggestions.isFetchingNextPage} onClick={() => suggestions.fetchNextPage()}>
                Load more
              </Button>
            </div>
          ) : null}
        </>
      )}
    </>
  );
}
