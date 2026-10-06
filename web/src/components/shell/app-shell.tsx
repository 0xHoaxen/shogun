"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import { useMutation, useQuery } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import type { ReactNode } from "react";

import { Rail } from "@/components/shell/rail";
import { Button } from "@/components/ui/button";
import { AuthService } from "@/gen/shogun/api/v1/auth_pb";
import { LOGIN_PATH } from "@/lib/transport";
import { cn } from "@/lib/utils";

interface NavItem {
  href: string;
  label: string;
  // The strip's section code and the rail's rotated name for the page.
  section: string;
  railLabel: string;
}

const NAV_ITEMS: readonly NavItem[] = [
  { href: "/jobs", label: "Jobs", section: "S2 / Tracker", railLabel: "JOB PIPELINE" },
  { href: "/contacts", label: "Contacts", section: "S3 / Network", railLabel: "CONTACT BOOK" },
  { href: "/drafts", label: "Drafts", section: "S4 / Drafts", railLabel: "APPROVAL QUEUE" },
  { href: "/settings/mail", label: "Mail", section: "S9 / Settings", railLabel: "MAIL ACCOUNT" },
  { href: "/settings/spend", label: "Spend", section: "S9 / Settings", railLabel: "SPEND LEDGER" },
];

interface AppShellProps {
  children: ReactNode;
}

// The signed-in frame: rail, strip, tab nav and utility footer. Nothing is
// shown until the session resolves; the transport sends a missing or expired
// session to /login.
export function AppShell({ children }: AppShellProps) {
  const pathname = usePathname();
  const session = useQuery(AuthService.method.getSession);
  const router = useRouter();
  const queryClient = useQueryClient();
  const logout = useMutation(AuthService.method.logout, {
    // Leave even when the call fails: the cookie is HttpOnly and torii expires it.
    onSettled: () => {
      queryClient.clear();
      router.replace(LOGIN_PATH);
    },
  });

  if (session.isPending) {
    return <div aria-busy="true" className="min-h-screen bg-paper" />;
  }
  if (session.isError) {
    if (ConnectError.from(session.error).code === Code.Unauthenticated) {
      return <div className="min-h-screen bg-paper" />;
    }
    return (
      <div className="flex min-h-screen flex-col items-start gap-4 bg-paper p-[22px]">
        <p role="alert">Can&apos;t reach Shogun. Try again.</p>
        <Button onClick={() => session.refetch()}>Retry</Button>
      </div>
    );
  }

  const current = NAV_ITEMS.find((item) => pathname.startsWith(item.href));
  const email = session.data.session?.email ?? "";

  return (
    <div className="grid min-h-screen grid-cols-[104px_minmax(0,1fr)] border border-ink bg-paper max-[860px]:grid-cols-1">
      <Rail label={current?.railLabel ?? "SHOGUN"} />
      <div className="flex min-w-0 flex-col">
        <div className="label flex min-h-11 flex-wrap items-center justify-between gap-3 border-b border-ink bg-strip px-[22px]">
          <span>{current?.section ?? "Shogun"}</span>
          <span>Shogun / Systems for career</span>
        </div>
        <nav aria-label="Shogun" className="flex flex-wrap border-b border-ink">
          {NAV_ITEMS.map((item) => {
            const active = item === current;
            return (
              <Link
                key={item.href}
                href={item.href}
                aria-current={active ? "page" : undefined}
                className={cn(
                  "label inline-flex min-h-11 grow items-center justify-center border-r border-ink px-4 font-medium last:border-r-0 hover:bg-ink hover:text-paper",
                  active && "bg-ink text-paper",
                )}
              >
                {item.label}
              </Link>
            );
          })}
        </nav>
        <main className="min-w-0 grow">{children}</main>
        <footer
          aria-label="Utility"
          className="flex min-h-11 flex-wrap items-center justify-end gap-3 border-t border-ink px-[22px]"
        >
          <span data-testid="owner-email">{email}</span>
          <Button onClick={() => logout.mutate({})}>Logout</Button>
        </footer>
      </div>
    </div>
  );
}
