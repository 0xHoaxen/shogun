"use client";

import { ConnectError, createClient } from "@connectrpc/connect";
import { useTransport } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useMemo, useState } from "react";

import { contactQueryKey, contactsQueryKey } from "@/components/contacts/contacts-key";
import { useUndoWindow } from "@/components/contacts/use-undo-window";
import { ContactsService } from "@/gen/shogun/api/v1/contacts_pb";
import { reasonOf } from "@/lib/errors";

interface Pending {
  id: string;
  version: number;
  on: string;
}

export interface MarkContacted {
  // pending is who is marked and not yet sent, with the date shown for them.
  pending: { id: string; on: string } | null;
  // error is what went wrong the last time a mark was sent, in words.
  error: string;
  mark: (contact: { id: string; fullName: string; version: number }, on: string) => void;
  // flush sends a waiting mark now. Call it before anything that would leave the
  // person, or replace the Undo toast.
  flush: () => void;
}

// errorText says what to do when a mark could not be sent.
function errorText(error: unknown): string {
  if (reasonOf(error) === "VERSION_CONFLICT") {
    return "This person changed elsewhere, so the page was reloaded. Mark them contacted again.";
  }
  return "Couldn't record that. Try again.";
}

// useMarkContacted records "I contacted them" with an Undo. The server cannot
// undo it (a status never moves back), so the call waits as long as the toast
// does (see useUndoWindow): Undo cancels it and nothing was ever sent.
export function useMarkContacted(): MarkContacted {
  const transport = useTransport();
  const client = useMemo(() => createClient(ContactsService, transport), [transport]);
  const queryClient = useQueryClient();
  const [pending, setPending] = useState<MarkContacted["pending"]>(null);
  const [error, setError] = useState("");

  const send = useCallback(
    async (item: Pending) => {
      try {
        await client.markContacted({ id: item.id, version: item.version, on: item.on });
        setError("");
      } catch (caught) {
        setError(errorText(ConnectError.from(caught)));
      } finally {
        setPending((current) => (current?.id === item.id ? null : current));
        // Whatever happened, the server's version of the person is the truth.
        await queryClient.invalidateQueries({ queryKey: contactsQueryKey });
        await queryClient.invalidateQueries({ queryKey: contactQueryKey });
      }
    },
    [client, queryClient],
  );
  const undoWindow = useUndoWindow<Pending>(send);

  const mark = useCallback<MarkContacted["mark"]>(
    (contact, on) => {
      setError("");
      setPending({ id: contact.id, on });
      undoWindow.start({ id: contact.id, version: contact.version, on }, `${contact.fullName} marked as contacted`, () =>
        setPending(null),
      );
    },
    [undoWindow],
  );

  return { pending, error, mark, flush: undoWindow.flush };
}
