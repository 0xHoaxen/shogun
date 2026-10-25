"use client";

import type { MessageInitShape } from "@bufbuild/protobuf";
import { ConnectError, createClient } from "@connectrpc/connect";
import { useTransport } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useMemo, useState } from "react";

import { contactsQueryKey } from "@/components/contacts/contacts-key";
import { importErrorText } from "@/components/contacts/import-errors";
import { people } from "@/components/contacts/import-fields";
import { useUndoWindow } from "@/components/contacts/use-undo-window";
import { useToast } from "@/components/ui/toast";
import { ContactsService } from "@/gen/shogun/api/v1/contacts_pb";

// An ImportJob is the request to send and how many people it will import, for the toast.
export interface ImportJob {
  request: MessageInitShape<typeof ContactsService.method.importContacts.input>;
  count: number;
}

export interface ImportPeople {
  // start queues the import behind an Undo. It is written when the toast ends.
  start: (job: ImportJob) => void;
  // error is what went wrong the last time an import was sent, in words.
  error: string;
}

// useImportPeople imports the people the owner ticked, with an Undo. Nothing is
// written until the toast's time is up, so Undo only cancels the call (see
// useUndoWindow). It lives on the People screen so that closing the panel does
// not cancel it.
export function useImportPeople(): ImportPeople {
  const transport = useTransport();
  const client = useMemo(() => createClient(ContactsService, transport), [transport]);
  const queryClient = useQueryClient();
  const toast = useToast();
  const [error, setError] = useState("");

  const send = useCallback(
    async (job: ImportJob) => {
      try {
        const response = await client.importContacts({ ...job.request, dryRun: false });
        const report = response.report;
        const created = report?.rowsCreated ?? 0;
        const updated = report?.rowsUpdated ?? 0;
        setError("");
        toast.show({
          text: updated > 0 ? `${people(created)} imported, ${updated} updated` : `${people(created)} imported`,
        });
      } catch (caught) {
        const text = importErrorText(ConnectError.from(caught));
        setError(text);
        toast.show({ text: "The import did not finish. Nothing was lost; try again." });
      } finally {
        await queryClient.invalidateQueries({ queryKey: contactsQueryKey });
      }
    },
    [client, queryClient, toast],
  );
  const undoWindow = useUndoWindow<ImportJob>(send);

  const start = useCallback<ImportPeople["start"]>(
    (job) => {
      setError("");
      undoWindow.start(job, `Importing ${people(job.count)}`);
    },
    [undoWindow],
  );

  return { start, error };
}
