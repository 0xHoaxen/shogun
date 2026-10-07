import { createConnectQueryKey } from "@connectrpc/connect-query";

import { LearningService } from "@/gen/shogun/api/v1/learning_pb";

// learningItemsKey matches every cached ListLearningItems result, whatever the
// filter, so a change refetches the items.
export const learningItemsKey = createConnectQueryKey({
  schema: LearningService.method.listLearningItems,
  cardinality: "infinite",
});

// learningActivitiesKey matches every cached ListLearningActivities result.
export const learningActivitiesKey = createConnectQueryKey({
  schema: LearningService.method.listLearningActivities,
  cardinality: "infinite",
});
