"use client";

import { useInfiniteQuery } from "@connectrpc/connect-query";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";

import { ActivitiesTable } from "@/components/learning/activities-table";
import { AddItemDialog } from "@/components/learning/add-item-dialog";
import { learningErrorText } from "@/components/learning/errors";
import { ItemsTable } from "@/components/learning/items-table";
import { STATUS_FILTERS, itemStatusLabel } from "@/components/learning/labels";
import { learningItemsKey } from "@/components/learning/learning-key";
import { LogActivityDialog } from "@/components/learning/log-activity-dialog";
import { PostDialog } from "@/components/learning/post-dialog";
import { useLearningClient } from "@/components/learning/use-learning-client";
import { PageHeader } from "@/components/shell/page-header";
import { Button } from "@/components/ui/button";
import { ItemStatus, LearningService, type LearningItem } from "@/gen/shogun/api/v1/learning_pb";
import { cn } from "@/lib/utils";

const PAGE_SIZE = 30;
// MAX_POST_ACTIVITIES is how many activities one post can be written from; dojo
// refuses more.
const MAX_POST_ACTIVITIES = 10;

export function LearningScreen() {
  const client = useLearningClient();
  const queryClient = useQueryClient();
  const [status, setStatus] = useState<ItemStatus>(ItemStatus.UNSPECIFIED);
  const [addingItem, setAddingItem] = useState(false);
  const [logging, setLogging] = useState(false);
  const [writing, setWriting] = useState(false);
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set());

  const items = useInfiniteQuery(
    LearningService.method.listLearningItems,
    { status, pageSize: PAGE_SIZE, pageToken: "" },
    { pageParamKey: "pageToken", getNextPageParam: (last) => last.nextPageToken || undefined },
  );
  // The dialog's choices ignore the filter, so a done item can still be logged against.
  const allItems = useInfiniteQuery(
    LearningService.method.listLearningItems,
    { status: ItemStatus.UNSPECIFIED, pageSize: PAGE_SIZE, pageToken: "" },
    { pageParamKey: "pageToken", getNextPageParam: (last) => last.nextPageToken || undefined },
  );
  const activities = useInfiniteQuery(
    LearningService.method.listLearningActivities,
    { itemId: "", pageSize: PAGE_SIZE, pageToken: "" },
    { pageParamKey: "pageToken", getNextPageParam: (last) => last.nextPageToken || undefined },
  );

  const itemRows = items.data?.pages.flatMap((page) => page.items) ?? [];
  const choices = useMemo(() => allItems.data?.pages.flatMap((page) => page.items) ?? [], [allItems.data]);
  const activityRows = activities.data?.pages.flatMap((page) => page.activities) ?? [];
  const itemTitles = useMemo(() => new Map(choices.map((item) => [item.id, item.title])), [choices]);

  const move = useMutation({
    mutationFn: ({ item, to }: { item: LearningItem; to: ItemStatus }) =>
      client.changeLearningItemStatus({ id: item.id, toStatus: to, version: item.version }),
    // Whatever the outcome, show what dojo holds now.
    onSettled: () => queryClient.invalidateQueries({ queryKey: learningItemsKey }),
  });

  function toggle(id: string) {
    setSelected((current) => {
      const next = new Set(current);
      if (!next.delete(id)) next.add(id);
      return next;
    });
  }

  return (
    <>
      <PageHeader
        title="What you're learning."
        description="Courses, books, projects and skills, and what you did on them. Log it, and Shogun drafts a post for you to approve."
      >
        <Button variant="primary" onClick={() => setAddingItem(true)}>
          Add item
        </Button>
        <Button onClick={() => setLogging(true)}>Log activity</Button>
      </PageHeader>

      <div role="group" aria-label="Filter by status" className="flex flex-wrap border-b border-ink p-[22px] pb-[23px]">
        {STATUS_FILTERS.map((filter) => (
          <button
            key={filter}
            type="button"
            aria-pressed={status === filter}
            onClick={() => setStatus(filter)}
            className={cn(
              "label -mr-px -mb-px inline-flex min-h-9 cursor-pointer items-center border border-ink px-3 font-medium hover:bg-strip",
              status === filter && "bg-ink text-paper hover:bg-ink",
            )}
          >
            {itemStatusLabel(filter)}
          </button>
        ))}
      </div>

      {move.isError ? (
        <p role="alert" className="border-b border-ink p-[22px]">
          {learningErrorText(move.error, "Couldn't move the item. Try again.")}
        </p>
      ) : null}

      {items.isError ? (
        <div className="flex items-center gap-3 border-b border-ink p-[22px]">
          <p role="alert">Couldn&apos;t load your items.</p>
          <Button onClick={() => items.refetch()}>Retry</Button>
        </div>
      ) : (
        <>
          <ItemsTable
            items={itemRows}
            busyId={move.isPending ? move.variables.item.id : null}
            onMove={(item, to) => move.mutate({ item, to })}
          />
          {items.isSuccess && itemRows.length === 0 ? <p className="border-b border-ink p-[22px] text-muted-ink">Nothing here.</p> : null}
          {items.hasNextPage ? (
            <div className="border-b border-ink p-[22px]">
              <Button disabled={items.isFetchingNextPage} onClick={() => items.fetchNextPage()}>
                Load more items
              </Button>
            </div>
          ) : null}
        </>
      )}

      <div className="flex items-center justify-between gap-3 border-b border-ink p-[22px]">
        <h2 className="h-display text-[34px]">Activity</h2>
        <Button variant="primary" disabled={selected.size === 0} onClick={() => setWriting(true)}>
          Write a post{selected.size > 0 ? ` (${selected.size})` : ""}
        </Button>
      </div>

      {activities.isError ? (
        <div className="flex items-center gap-3 border-b border-ink p-[22px]">
          <p role="alert">Couldn&apos;t load your activity.</p>
          <Button onClick={() => activities.refetch()}>Retry</Button>
        </div>
      ) : (
        <>
          <ActivitiesTable
            activities={activityRows}
            itemTitles={itemTitles}
            selected={selected}
            atLimit={selected.size >= MAX_POST_ACTIVITIES}
            onToggle={toggle}
          />
          {activities.isSuccess && activityRows.length === 0 ? (
            <p className="border-b border-ink p-[22px] text-muted-ink">No activity yet.</p>
          ) : null}
          {activities.hasNextPage ? (
            <div className="border-b border-ink p-[22px]">
              <Button disabled={activities.isFetchingNextPage} onClick={() => activities.fetchNextPage()}>
                Load more activity
              </Button>
            </div>
          ) : null}
        </>
      )}

      <AddItemDialog open={addingItem} onOpenChange={setAddingItem} />
      <LogActivityDialog open={logging} onOpenChange={setLogging} items={choices} />
      <PostDialog
        open={writing}
        onOpenChange={(open) => {
          setWriting(open);
          if (!open) setSelected(new Set());
        }}
        activityIds={[...selected]}
      />
    </>
  );
}
