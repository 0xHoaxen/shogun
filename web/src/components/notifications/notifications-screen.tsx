"use client";

import { useInfiniteQuery, useMutation, useQuery } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import Link from "next/link";

import { notificationTag } from "@/components/notifications/labels";
import { notificationsKey } from "@/components/notifications/notifications-key";
import { PageHeader } from "@/components/shell/page-header";
import { Button } from "@/components/ui/button";
import { NotificationsService, type Notification } from "@/gen/shogun/api/v1/notifications_pb";
import { groupByDay, notificationTime, unreadHeadline } from "@/lib/notifications";
import { cn } from "@/lib/utils";

const PAGE_SIZE = 30;
const ROW = "grid grid-cols-[96px_minmax(0,1fr)_64px] items-center gap-3 border-b border-ink px-[22px] py-3 max-[860px]:grid-cols-1";

export function NotificationsScreen() {
  const queryClient = useQueryClient();
  const refresh = () => queryClient.invalidateQueries({ queryKey: notificationsKey });
  const unread = useQuery(NotificationsService.method.listNotifications, { unreadOnly: true, pageSize: 1 });
  const list = useInfiniteQuery(
    NotificationsService.method.listNotifications,
    { pageSize: PAGE_SIZE, pageToken: "" },
    { pageParamKey: "pageToken", getNextPageParam: (last) => last.nextPageToken || undefined },
  );
  const markAll = useMutation(NotificationsService.method.markAllNotificationsRead, { onSuccess: refresh });
  const markRead = useMutation(NotificationsService.method.markNotificationsRead, { onSuccess: refresh });

  const count = unread.data?.unreadCount ?? 0;
  const items = list.data?.pages.flatMap((page) => page.notifications) ?? [];
  const groups = groupByDay(items, new Date());

  return (
    <>
      <PageHeader
        title={unreadHeadline(count)}
        description="Things that are ready, due, or close to a limit."
      >
        <Button disabled={count === 0 || markAll.isPending} onClick={() => markAll.mutate({})}>
          Mark all read
        </Button>
      </PageHeader>

      {list.isError ? (
        <p role="alert" className="p-[22px]">
          Can&apos;t load notifications. Try again.
        </p>
      ) : list.isPending ? (
        <div aria-busy="true" className="min-h-40" />
      ) : items.length === 0 ? (
        <p className="p-[22px] text-muted-ink">Nothing yet. Notifications show up here as they happen.</p>
      ) : (
        <ul aria-label="Notifications">
          {groups.map((group) => (
            <li key={group.heading} className="list-none">
              <h2 className="label flex min-h-9 items-center bg-ink px-[22px] font-medium text-paper">
                {group.heading}
              </h2>
              <ul>
                {group.items.map((n) => (
                  <NotificationRow key={n.id} notification={n} onOpen={() => !n.readAt && markRead.mutate({ ids: [n.id] })} />
                ))}
              </ul>
            </li>
          ))}
        </ul>
      )}

      {list.hasNextPage ? (
        <div className="p-[22px]">
          <Button disabled={list.isFetchingNextPage} onClick={() => list.fetchNextPage()}>
            Load more
          </Button>
        </div>
      ) : null}
    </>
  );
}

interface NotificationRowProps {
  notification: Notification;
  onOpen: () => void;
}

function NotificationRow({ notification: n, onOpen }: NotificationRowProps) {
  const unread = !n.readAt;
  const content = (
    <>
      <span className={cn("label inline-flex min-h-5 w-fit items-center border border-ink px-2", unread && "border-signal bg-signal font-bold")}>
        {notificationTag(n.type)}
      </span>
      <span>
        <span className={unread ? "font-bold" : "font-normal"}>{n.title}</span>
        {n.body ? <span className="block opacity-75">{n.body}</span> : null}
      </span>
      <span className="text-right max-[860px]:text-left">{notificationTime(n, new Date())}</span>
    </>
  );
  return (
    <li data-unread={unread} className="list-none">
      {n.link ? (
        <Link href={n.link} onClick={onOpen} className={cn(ROW, "hover:bg-ink hover:text-paper")}>
          {content}
        </Link>
      ) : (
        <div className={ROW}>{content}</div>
      )}
    </li>
  );
}
