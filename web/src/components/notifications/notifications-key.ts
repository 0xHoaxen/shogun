import { createConnectQueryKey } from "@connectrpc/connect-query";

import { NotificationsService } from "@/gen/shogun/api/v1/notifications_pb";

// notificationsKey matches every cached ListNotifications result, the bell's
// unread count and the screen's pages alike, so a change refetches them all.
export const notificationsKey = createConnectQueryKey({
  schema: NotificationsService.method.listNotifications,
  cardinality: undefined,
});
