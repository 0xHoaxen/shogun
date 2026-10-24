"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import { useMutation, useQuery } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { usePathname, useRouter } from "next/navigation";
import { type ReactNode, useEffect, useState } from "react";

import { NotificationBell } from "@/components/notifications/notification-bell";
import { FooterStrip } from "@/components/shell/footer-strip";
import { Sidebar } from "@/components/shell/sidebar";
import { TopBar } from "@/components/shell/top-bar";
import { Button } from "@/components/ui/button";
import { ToastProvider } from "@/components/ui/toast";
import { DEFAULT_SCHEME, NAV_SECTIONS } from "@/content/sections";
import { AuthService } from "@/gen/shogun/api/v1/auth_pb";
import { DraftState, DraftsService } from "@/gen/shogun/api/v1/drafts_pb";
import { LOGIN_PATH } from "@/lib/transport";

// One page of pending drafts is enough to count them up to the chip's cap.
const INBOX_PAGE_SIZE = 99;
const INBOX_CAPPED_LABEL = `${INBOX_PAGE_SIZE}+`;

interface AppShellProps {
  children: ReactNode;
}

function nextStatus(label: string | null): string {
  if (label === null) return "Nothing is waiting for you";
  return `${label} ${label === "1" ? "draft is" : "drafts are"} waiting for your approval`;
}

// The signed-in frame: top bar, numbered sidebar and footer strip. Nothing is
// shown until the session resolves; the transport sends a missing or expired
// session to /login.
export function AppShell({ children }: AppShellProps) {
  const pathname = usePathname();
  const session = useQuery(AuthService.method.getSession);
  const router = useRouter();
  const queryClient = useQueryClient();
  const [collapsed, setCollapsed] = useState(false);
  const pending = useQuery(DraftsService.method.listQueue, {
    state: DraftState.PENDING,
    pageSize: INBOX_PAGE_SIZE,
    pageToken: "",
  });
  const logout = useMutation(AuthService.method.logout, {
    // Leave even when the call fails: the cookie is HttpOnly and torii expires it.
    onSettled: () => {
      queryClient.clear();
      router.replace(LOGIN_PATH);
    },
  });

  const current = NAV_SECTIONS.find((item) => pathname.startsWith(item.href));
  const scheme = current?.scheme ?? DEFAULT_SCHEME;
  // Panels and dialogs render in a portal outside the shell, so the scheme
  // also goes on <html> for them to pick up.
  useEffect(() => {
    document.documentElement.dataset.scheme = scheme;
  }, [scheme]);

  if (session.isPending) {
    return <div aria-busy="true" className="min-h-screen bg-background" />;
  }
  if (session.isError) {
    if (ConnectError.from(session.error).code === Code.Unauthenticated) {
      return <div className="min-h-screen bg-background" />;
    }
    return (
      <div className="flex min-h-screen flex-col items-start gap-4 bg-background p-[22px]">
        <p role="alert">Can&apos;t reach Shogun. Try again.</p>
        <Button onClick={() => session.refetch()}>Retry</Button>
      </div>
    );
  }

  const pendingCount = pending.data?.drafts.length ?? 0;
  const hasMore = Boolean(pending.data?.nextPageToken);
  const inboxLabel = hasMore ? INBOX_CAPPED_LABEL : pendingCount > 0 ? String(pendingCount) : null;
  const email = session.data.session?.email ?? "";

  return (
    <ToastProvider>
      <div data-scheme={scheme} className="flex min-h-screen flex-col bg-background">
        <TopBar status={nextStatus(inboxLabel)} />
        <div className="flex min-h-0 grow max-[860px]:flex-col">
          <Sidebar
            current={current}
            inboxLabel={inboxLabel}
            collapsed={collapsed}
            onToggle={() => setCollapsed((value) => !value)}
          />
          <main className="min-w-0 grow">{children}</main>
        </div>
        <FooterStrip scheme={scheme}>
          <NotificationBell active={false} />
          <span data-testid="owner-email">{email}</span>
          <Button onClick={() => logout.mutate({})}>Logout</Button>
        </FooterStrip>
      </div>
    </ToastProvider>
  );
}
