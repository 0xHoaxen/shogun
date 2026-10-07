import { timestampDate } from "@bufbuild/protobuf/wkt";

import type { Notification } from "@/gen/shogun/api/v1/notifications_pb";
import { ownerToday } from "@/lib/days";

const OWNER_TIME_ZONE = "Asia/Kolkata";

const timeFormat = new Intl.DateTimeFormat("en-GB", {
  timeZone: OWNER_TIME_ZONE,
  hour: "2-digit",
  minute: "2-digit",
});
const dateFormat = new Intl.DateTimeFormat("en-GB", {
  timeZone: OWNER_TIME_ZONE,
  day: "numeric",
  month: "short",
});

export interface NotificationGroup {
  heading: "Today" | "Earlier";
  items: readonly Notification[];
}

function createdAt(n: Notification): Date | undefined {
  return n.createdAt ? timestampDate(n.createdAt) : undefined;
}

// groupByDay splits notifications, newest first as given, into those from the
// owner's current day and the rest. A group with nothing in it is left out.
export function groupByDay(items: readonly Notification[], now: Date): NotificationGroup[] {
  const today = ownerToday(now);
  const todays: Notification[] = [];
  const earlier: Notification[] = [];
  for (const n of items) {
    const at = createdAt(n);
    (at && ownerToday(at) === today ? todays : earlier).push(n);
  }
  const groups: NotificationGroup[] = [
    { heading: "Today", items: todays },
    { heading: "Earlier", items: earlier },
  ];
  return groups.filter((group) => group.items.length > 0);
}

// notificationTime is the clock time for a notification from today and the date
// for an older one, in the owner's zone.
export function notificationTime(n: Notification, now: Date): string {
  const at = createdAt(n);
  if (!at) return "";
  return ownerToday(at) === ownerToday(now) ? timeFormat.format(at) : dateFormat.format(at);
}

// unreadHeadline is the page title for an unread count.
export function unreadHeadline(count: number): string {
  return count === 0 ? "All caught up." : `${count} unread.`;
}
