"use client";

import { ConnectError, createClient } from "@connectrpc/connect";
import { useTransport } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { contactQueryKey, contactsQueryKey } from "@/components/contacts/contacts-key";
import { TOAST_MS, useToast } from "@/components/ui/toast";
import { ContactsService } from "@/gen/shogun/api/v1/contacts_pb";
import { reasonOf } from "@/lib/errors";

interface Pending {
  id: string;
  name: string;
  version: number;
  on: string;
  timer: ReturnType<typeof setTimeout>;
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
// does: Undo cancels it and nothing was ever sent. A mark that is still waiting
// is sent at once when the person is left, another is made, the tab is hidden or
// the page goes, so it is never lost.
export function useMarkContacted(): MarkContacted {
  const transport = useTransport();
  const client = useMemo(() => createClient(ContactsService, transport), [transport]);
  const queryClient = useQueryClient();
  const toast = useToast();
  const waiting = useRef<Pending | null>(null);
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

  const flush = useCallback(() => {
    const item = waiting.current;
    if (item === null) {
      return;
    }
    clearTimeout(item.timer);
    waiting.current = null;
    void send(item);
  }, [send]);

  const mark = useCallback<MarkContacted["mark"]>(
    (contact, on) => {
      flush();
      setError("");
      const item: Pending = {
        id: contact.id,
        name: contact.fullName,
        version: contact.version,
        on,
        timer: setTimeout(flush, TOAST_MS),
      };
      waiting.current = item;
      setPending({ id: item.id, on });
      toast.show({
        text: `${contact.fullName} marked as contacted`,
        undo: () => {
          if (waiting.current === item) {
            clearTimeout(item.timer);
            waiting.current = null;
            setPending(null);
          }
        },
      });
    },
    [flush, toast],
  );

  useEffect(() => {
    const onHide = () => {
      if (document.visibilityState === "hidden") {
        flush();
      }
    };
    document.addEventListener("visibilitychange", onHide);
    window.addEventListener("pagehide", flush);
    return () => {
      document.removeEventListener("visibilitychange", onHide);
      window.removeEventListener("pagehide", flush);
      flush();
    };
  }, [flush]);

  return { pending, error, mark, flush };
}
