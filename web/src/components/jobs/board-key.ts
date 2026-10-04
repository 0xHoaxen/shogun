import { createConnectQueryKey } from "@connectrpc/connect-query";

import { JobsService } from "@/gen/shogun/api/v1/jobs_pb";

// boardQueryKey matches every cached GetBoard result, for optimistic updates
// and for refetching after a change.
export const boardQueryKey = createConnectQueryKey({
  schema: JobsService.method.getBoard,
  cardinality: "finite",
});
