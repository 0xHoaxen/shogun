import { createConnectQueryKey } from "@connectrpc/connect-query";

import { DraftsService } from "@/gen/shogun/api/v1/drafts_pb";

// draftsQueueKey matches every cached ListQueue result, whatever the state, so
// a change refetches the queue.
export const draftsQueueKey = createConnectQueryKey({
  schema: DraftsService.method.listQueue,
  cardinality: "infinite",
});

// draftKey matches every cached GetDraft result.
export const draftKey = createConnectQueryKey({
  schema: DraftsService.method.getDraft,
  cardinality: "finite",
});
