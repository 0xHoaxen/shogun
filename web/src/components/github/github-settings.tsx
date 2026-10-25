"use client";

import { ConnectError } from "@connectrpc/connect";
import { createConnectQueryKey, useMutation, useQuery } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";

import { PageHeader } from "@/components/shell/page-header";
import { Button } from "@/components/ui/button";
import { SettingsCard } from "@/components/settings/settings-card";
import { SECTIONS } from "@/content/sections";
import { ProfileService } from "@/gen/shogun/api/v1/profile_pb";
import { reasonOf } from "@/lib/errors";

const REASON_GITHUB_NOT_CONFIGURED = "GITHUB_NOT_CONFIGURED";

const accountKey = createConnectQueryKey({
  schema: ProfileService.method.getGitHubAccount,
  cardinality: "finite",
});

// safeAuthUrl returns url only when it is an https address. The URL comes from
// our own backend, but a page that sends the browser away should never follow
// anything else.
function safeAuthUrl(url: string): string | null {
  try {
    return new URL(url).protocol === "https:" ? url : null;
  } catch {
    return null;
  }
}

function connectFailure(error: unknown): string {
  if (reasonOf(error) === REASON_GITHUB_NOT_CONFIGURED) {
    return "Connecting GitHub is not set up on this Shogun yet. Ask the person who runs it to add a GitHub OAuth app.";
  }
  return ConnectError.from(error).rawMessage;
}

export function GitHubSettings() {
  const queryClient = useQueryClient();
  const account = useQuery(ProfileService.method.getGitHubAccount);
  const connect = useMutation(ProfileService.method.connectGitHub, {
    onSuccess: (response) => {
      const target = safeAuthUrl(response.authUrl);
      if (target) {
        window.location.assign(target);
      }
    },
  });
  const disconnect = useMutation(ProfileService.method.disconnectGitHub, {
    onSettled: () => queryClient.invalidateQueries({ queryKey: accountKey }),
  });
  const refused = connect.isSuccess && safeAuthUrl(connect.data.authUrl) === null;
  const connected = account.data?.connected === true;

  return (
    <>
      <PageHeader
        about={SECTIONS.github}
        title="Your GitHub."
        description="Connect GitHub so Shogun can suggest resume and LinkedIn edits from what you have built."
      />
      <SettingsCard id="github" label="GitHub" note="Public profile only">
        <div className="grid gap-3.5 p-[22px]">
        <p className="max-w-[60ch] text-muted-foreground">
          Shogun asks GitHub for no permissions beyond your public profile, so it can read only what anyone can
          see and can never change anything. Your token is stored encrypted and used only for your account.
          Disconnecting deletes it.
        </p>
        {account.isError ? (
          <div className="flex items-center gap-3">
            <p role="alert">Couldn&apos;t load your GitHub connection.</p>
            <Button onClick={() => account.refetch()}>Retry</Button>
          </div>
        ) : account.isPending ? (
          <p aria-busy="true">Loading…</p>
        ) : connected ? (
          <p role="status">
            Connected as <strong>{account.data.login}</strong>.
          </p>
        ) : (
          <p role="status">Not connected.</p>
        )}
        {connect.isError ? (
          <p role="alert" className="border border-line2 bg-panel px-3 py-2">
            {connectFailure(connect.error)}
          </p>
        ) : null}
        {disconnect.isError ? (
          <p role="alert" className="border border-line2 bg-panel px-3 py-2">
            Couldn&apos;t disconnect GitHub. Try again.
          </p>
        ) : null}
        {refused ? (
          <p role="alert" className="border border-line2 bg-panel px-3 py-2">
            GitHub&apos;s sign-in address could not be trusted, so nothing was opened.
          </p>
        ) : null}
        <div className="flex flex-wrap gap-2">
          <Button variant="primary" disabled={connect.isPending || account.isPending} onClick={() => connect.mutate({})}>
            {connected ? "Reconnect GitHub" : "Connect GitHub"}
          </Button>
          {connected ? (
            <Button disabled={disconnect.isPending} onClick={() => disconnect.mutate({})}>
              Disconnect GitHub
            </Button>
          ) : null}
        </div>
        </div>
      </SettingsCard>
    </>
  );
}
