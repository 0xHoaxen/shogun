"use client";

import { Code, ConnectError, createClient } from "@connectrpc/connect";
import { createConnectQueryKey, useTransport } from "@connectrpc/connect-query";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useEffect } from "react";

import { notificationsKey } from "@/components/notifications/notifications-key";
import { AuthService } from "@/gen/shogun/api/v1/auth_pb";
import { NotificationsService } from "@/gen/shogun/api/v1/notifications_pb";

const RECONNECT_BASE_MS = 1_000;
const RECONNECT_MAX_MS = 30_000;

const sessionKey = createConnectQueryKey({ schema: AuthService.method.getSession, cardinality: undefined });

function reconnectDelay(failures: number): number {
  return Math.min(RECONNECT_MAX_MS, RECONNECT_BASE_MS * 2 ** failures);
}

// sleep resolves after ms, or at once when signal aborts.
function sleep(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    const timer = setTimeout(resolve, ms);
    signal.addEventListener(
      "abort",
      () => {
        clearTimeout(timer);
        resolve();
      },
      { once: true },
    );
  });
}

// useNotificationStream keeps one Stream open while the component is mounted
// and refetches the notification lists whenever something new arrives. When the
// stream ends, which torii does now and then and on any hiccup, it reconnects
// from the last id it saw, so nothing is missed. Failures back off up to 30
// seconds. A refused session stops it and rechecks the session, which sends the
// browser to the login page.
export function useNotificationStream(): void {
  const transport = useTransport();
  const queryClient = useQueryClient();

  useEffect(() => {
    const controller = new AbortController();
    void follow(createClient(NotificationsService, transport), queryClient, controller.signal);
    return () => controller.abort();
  }, [transport, queryClient]);
}

async function follow(
  client: ReturnType<typeof createClient<typeof NotificationsService>>,
  queryClient: QueryClient,
  signal: AbortSignal,
): Promise<void> {
  let lastSeenId = "";
  let failures = 0;
  while (!signal.aborted) {
    try {
      for await (const message of client.stream({ lastSeenId }, { signal })) {
        failures = 0;
        lastSeenId = message.notification?.id ?? lastSeenId;
        await queryClient.invalidateQueries({ queryKey: notificationsKey });
      }
    } catch (error) {
      if (signal.aborted) return;
      if (ConnectError.from(error).code === Code.Unauthenticated) {
        await queryClient.invalidateQueries({ queryKey: sessionKey });
        return;
      }
      failures += 1;
    }
    await sleep(reconnectDelay(failures), signal);
  }
}
