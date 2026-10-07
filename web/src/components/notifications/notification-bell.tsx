"use client";

import { useQuery } from "@connectrpc/connect-query";
import Link from "next/link";

import { useNotificationStream } from "@/components/notifications/use-notification-stream";
import { NotificationsService } from "@/gen/shogun/api/v1/notifications_pb";
import { cn } from "@/lib/utils";

export const NOTIFICATIONS_PATH = "/notifications";

interface NotificationBellProps {
  active: boolean;
}

// The bell in the utility footer: a link to the notifications page with the
// unread count. It also keeps the live stream open, so the count follows new
// notifications on every page.
export function NotificationBell({ active }: NotificationBellProps) {
  useNotificationStream();
  // One row is enough: the response carries the total unread count.
  const unread = useQuery(NotificationsService.method.listNotifications, {
    unreadOnly: true,
    pageSize: 1,
  });
  const count = unread.data?.unreadCount ?? 0;

  return (
    <Link
      href={NOTIFICATIONS_PATH}
      aria-current={active ? "page" : undefined}
      aria-label={count > 0 ? `Notifications, ${count} unread` : "Notifications"}
      className={cn(
        "label inline-flex min-h-9 items-center gap-2 border border-ink px-3.5 font-bold hover:bg-ink hover:text-paper",
        active && "bg-ink text-paper",
      )}
    >
      Bell
      {count > 0 ? (
        <span
          data-testid="unread-count"
          className="inline-flex min-h-5 items-center border border-signal bg-signal px-2 text-[11px] font-bold text-ink"
        >
          {count}
        </span>
      ) : null}
    </Link>
  );
}
