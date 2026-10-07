import { createConnectQueryKey } from "@connectrpc/connect-query";

import { ProfileService } from "@/gen/shogun/api/v1/profile_pb";

// suggestionsKey matches every cached ListProfileSuggestions result, whatever
// the filter, so a change refetches the list.
export const suggestionsKey = createConnectQueryKey({
  schema: ProfileService.method.listProfileSuggestions,
  cardinality: "infinite",
});
