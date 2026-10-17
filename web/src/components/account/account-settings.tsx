"use client";

import { ConnectError } from "@connectrpc/connect";
import { createConnectQueryKey, useMutation, useQuery } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { useMemo, useState } from "react";

import { PageHeader } from "@/components/shell/page-header";
import { Button } from "@/components/ui/button";
import { SelectField } from "@/components/ui/field";
import { SECTIONS } from "@/content/sections";
import { AuthService } from "@/gen/shogun/api/v1/auth_pb";
import { reasonOf } from "@/lib/errors";
import { LOGIN_PATH } from "@/lib/transport";

const sessionKey = createConnectQueryKey({ schema: AuthService.method.getSession, cardinality: "finite" });

// zoneNames lists the IANA zones this browser knows, with current always present.
function zoneNames(current: string): string[] {
  const known = Intl.supportedValuesOf("timeZone");
  return known.includes(current) ? known : [current, ...known];
}

function timezoneFailure(error: unknown): string {
  return reasonOf(error) === "INVALID_TIMEZONE"
    ? "That is not a time zone Shogun knows. Pick one from the list."
    : ConnectError.from(error).rawMessage;
}

interface TimezoneProps {
  current: string;
}

function TimezoneForm({ current }: TimezoneProps) {
  const queryClient = useQueryClient();
  const [choice, setChoice] = useState(current);
  const zones = useMemo(() => zoneNames(current), [current]);
  const browserZone = Intl.DateTimeFormat().resolvedOptions().timeZone;
  const save = useMutation(AuthService.method.setTimezone, {
    onSuccess: () => queryClient.invalidateQueries({ queryKey: sessionKey }),
  });

  return (
    <section aria-labelledby="timezone-heading" className="grid gap-3.5 border-b border-ink p-[22px]">
      <h2 id="timezone-heading" className="label font-bold">
        Your days
      </h2>
      <p className="max-w-[60ch] text-muted-ink">
        Your follow-up reminders, daily digest and daily summaries follow the days of this time zone.
      </p>
      <div className="max-w-sm">
        <SelectField label="Time zone" value={choice} onChange={(event) => setChoice(event.target.value)}>
          {zones.map((zone) => (
            <option key={zone} value={zone}>
              {zone}
            </option>
          ))}
        </SelectField>
      </div>
      {save.isError ? (
        <p role="alert" className="border border-ink bg-strip px-3 py-2">
          {timezoneFailure(save.error)}
        </p>
      ) : null}
      {save.isSuccess ? <p role="status">Time zone saved.</p> : null}
      <div className="flex flex-wrap gap-2">
        <Button variant="primary" disabled={save.isPending || choice === current} onClick={() => save.mutate({ timezone: choice })}>
          Save time zone
        </Button>
        {browserZone && browserZone !== choice ? (
          <Button onClick={() => setChoice(browserZone)}>Use this browser&apos;s ({browserZone})</Button>
        ) : null}
      </div>
    </section>
  );
}

function SignOutEverywhere() {
  const router = useRouter();
  const queryClient = useQueryClient();
  const signOut = useMutation(AuthService.method.logoutEverywhere, {
    onSuccess: () => {
      queryClient.clear();
      router.replace(LOGIN_PATH);
    },
  });

  return (
    <section aria-labelledby="sessions-heading" className="grid gap-3.5 border-b border-ink p-[22px]">
      <h2 id="sessions-heading" className="label font-bold">
        Sessions
      </h2>
      <p className="max-w-[60ch] text-muted-ink">
        Sign out on every device where you are signed in, this one included. Use it if you lost a device or
        used a shared computer.
      </p>
      {signOut.isError ? (
        <p role="alert" className="border border-ink bg-strip px-3 py-2">
          Couldn&apos;t sign you out everywhere. Try again.
        </p>
      ) : null}
      <div>
        <Button disabled={signOut.isPending} onClick={() => signOut.mutate({})}>
          Sign out everywhere
        </Button>
      </div>
    </section>
  );
}

export function AccountSettings() {
  const session = useQuery(AuthService.method.getSession);
  const timezone = session.data?.session?.timezone;

  return (
    <>
      <PageHeader
        about={SECTIONS.account}
        title="Your account."
        description="The time zone your days follow, and signing out on every device."
      />
      {session.isError ? (
        <div className="flex items-center gap-3 border-b border-ink p-[22px]">
          <p role="alert">Couldn&apos;t load your account.</p>
          <Button onClick={() => session.refetch()}>Retry</Button>
        </div>
      ) : timezone === undefined ? (
        <div aria-busy="true" className="min-h-40 border-b border-ink p-[22px]" />
      ) : (
        <TimezoneForm key={timezone} current={timezone} />
      )}
      <SignOutEverywhere />
    </>
  );
}
