"use client";

import { createConnectQueryKey, useMutation, useQuery } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { Button } from "@/components/ui/button";
import { TextField } from "@/components/ui/field";
import { NotificationsService, type NotificationSettings } from "@/gen/shogun/api/v1/notifications_pb";
import { REASON_VERSION_CONFLICT, reasonOf } from "@/lib/errors";
import { DEFAULT_QUIET_FROM, DEFAULT_QUIET_TO, minutesToTime, timeToMinutes } from "@/lib/quiet-hours";

const settingsQueryKey = createConnectQueryKey({
  schema: NotificationsService.method.getNotificationSettings,
  cardinality: "finite",
});

// useSaveSettings saves the settings and then refetches them. It lives above
// the form, because the form starts over when the version changes and would
// lose the result of the save it has just made.
function useSaveSettings() {
  const queryClient = useQueryClient();
  return useMutation(NotificationsService.method.saveNotificationSettings, {
    onSettled: () => queryClient.invalidateQueries({ queryKey: settingsQueryKey }),
  });
}

interface SettingsFormProps {
  settings: NotificationSettings;
  save: ReturnType<typeof useSaveSettings>;
}

// SettingsForm edits the settings it was given. The parent keys it by version,
// so a save or a conflict, which both refetch, start it again from what is stored.
function SettingsForm({ settings, save }: SettingsFormProps) {
  const [inAppEnabled, setInAppEnabled] = useState(settings.inAppEnabled);
  const [quietOn, setQuietOn] = useState(settings.quiet !== undefined);
  const [from, setFrom] = useState(settings.quiet ? minutesToTime(settings.quiet.fromMinute) : DEFAULT_QUIET_FROM);
  const [to, setTo] = useState(settings.quiet ? minutesToTime(settings.quiet.toMinute) : DEFAULT_QUIET_TO);

  const fromMinute = timeToMinutes(from);
  const toMinute = timeToMinutes(to);
  const windowProblem = !quietOn
    ? null
    : fromMinute === null || toMinute === null
      ? "Enter both times, such as 22:00 and 08:00."
      : fromMinute === toMinute
        ? "The start and the end must be different times."
        : null;
  const conflict = save.isError && reasonOf(save.error) === REASON_VERSION_CONFLICT;

  return (
    <form
      aria-label="How Shogun reaches you"
      className="flex max-w-xl flex-col gap-5 p-[22px]"
      onSubmit={(event) => {
        event.preventDefault();
        if (windowProblem) {
          return;
        }
        save.mutate({
          settings: {
            inAppEnabled,
            quiet: quietOn && fromMinute !== null && toMinute !== null ? { fromMinute, toMinute } : undefined,
            version: settings.version,
          },
        });
      }}
    >
      <label className="label flex min-h-10 items-center gap-2 text-[11px] font-medium">
        <input type="checkbox" checked={inAppEnabled} onChange={(event) => setInAppEnabled(event.target.checked)} />
        In-app notifications and the daily digest
      </label>
      <label className="label flex min-h-10 items-center gap-2 text-[11px] font-medium">
        <input type="checkbox" checked={quietOn} onChange={(event) => setQuietOn(event.target.checked)} />
        Quiet hours
      </label>
      {quietOn ? (
        <div className="flex flex-wrap items-end gap-4">
          <TextField label="Quiet from" type="time" value={from} onChange={(event) => setFrom(event.target.value)} />
          <TextField label="Quiet until" type="time" value={to} onChange={(event) => setTo(event.target.value)} />
        </div>
      ) : null}
      <p className="text-muted-ink">
        During quiet hours the daily digest waits until the window ends. Times are in India time (IST).
      </p>
      {windowProblem ? <p role="alert">{windowProblem}</p> : null}
      {conflict ? (
        <p role="alert">These settings changed elsewhere. They have been reloaded; check them and save again.</p>
      ) : save.isError ? (
        <p role="alert">Couldn&apos;t save your settings. Try again.</p>
      ) : null}
      {save.isSuccess ? <p role="status">Saved.</p> : null}
      <div>
        <Button type="submit" variant="primary" disabled={windowProblem !== null || save.isPending}>
          Save
        </Button>
      </div>
    </form>
  );
}

export function NotificationSettingsForm() {
  const settings = useQuery(NotificationsService.method.getNotificationSettings, {});
  const save = useSaveSettings();

  if (settings.isError) {
    return (
      <div className="flex items-center gap-3 p-[22px]">
        <p role="alert">Couldn&apos;t load your settings.</p>
        <Button onClick={() => settings.refetch()}>Retry</Button>
      </div>
    );
  }
  if (!settings.data?.settings) {
    return <p className="p-[22px] text-muted-ink">Loading…</p>;
  }
  return <SettingsForm key={String(settings.data.settings.version)} settings={settings.data.settings} save={save} />;
}
