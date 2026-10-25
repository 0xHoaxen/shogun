"use client";

import { createClient } from "@connectrpc/connect";
import { useTransport } from "@connectrpc/connect-query";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";

import { draftsQueueKey } from "@/components/drafts/drafts-key";
import type { Contact } from "@/gen/shogun/api/v1/contacts_pb";
import { DraftChannel, DraftKind, DraftsService, DraftTarget } from "@/gen/shogun/api/v1/drafts_pb";
import { reasonOf } from "@/lib/errors";

const IDEMPOTENCY_KEY_HEADER = "Idempotency-Key";

// channelFor picks how a draft to this person should go: their preferred
// channel, or email when they have an address and no preference. undefined
// means there is nothing to send to yet.
export function channelFor(contact: Pick<Contact, "preferredChannel" | "email">): DraftChannel | undefined {
  switch (contact.preferredChannel) {
    case "email":
      return DraftChannel.EMAIL;
    case "linkedin":
      return DraftChannel.LINKEDIN;
    case "x":
      return DraftChannel.X;
    case "phone":
    case "other":
      return DraftChannel.OTHER;
    default:
      return contact.email === "" ? undefined : DraftChannel.EMAIL;
  }
}

export interface DraftANote {
  pending: boolean;
  // done is true once a draft is waiting in Inbox.
  done: boolean;
  // problem is a plain-words reason nothing was drafted.
  problem: string;
  // needsProvider is true when the owner has no AI provider yet.
  needsProvider: boolean;
  start: (contact: Pick<Contact, "id" | "preferredChannel" | "email">) => void;
}

// useDraftANote asks fude for a short follow-up to a person. The draft goes to
// Inbox; nothing leaves until the owner approves it there.
export function useDraftANote(): DraftANote {
  const transport = useTransport();
  const client = useMemo(() => createClient(DraftsService, transport), [transport]);
  const queryClient = useQueryClient();
  const [problem, setProblem] = useState("");
  // One key per intended draft: pressing again after a failure returns the
  // original, and a new draft after a success gets a new key.
  const [key, setKey] = useState(() => crypto.randomUUID());

  const generate = useMutation({
    mutationFn: (input: { id: string; channel: DraftChannel; recipient: string }) =>
      client.generateDraft(
        {
          kind: DraftKind.FOLLOW_UP,
          target: DraftTarget.CONTACT,
          targetId: input.id,
          channel: input.channel,
          recipient: input.recipient,
          extraContext: "",
        },
        { headers: { [IDEMPOTENCY_KEY_HEADER]: key } },
      ),
    onSuccess: async () => {
      setKey(crypto.randomUUID());
      await queryClient.invalidateQueries({ queryKey: draftsQueueKey });
    },
  });

  const needsProvider = generate.isError && reasonOf(generate.error) === "AI_PROVIDER_NOT_CONFIGURED";
  return {
    pending: generate.isPending,
    done: generate.isSuccess,
    problem: problem || (generate.isError && !needsProvider ? "Couldn't draft a note. Try again." : ""),
    needsProvider,
    start: (contact) => {
      generate.reset();
      const channel = channelFor(contact);
      if (channel === undefined || (channel === DraftChannel.EMAIL && contact.email === "")) {
        setProblem("Add an email or a channel first.");
        return;
      }
      setProblem("");
      generate.mutate({ id: contact.id, channel, recipient: channel === DraftChannel.EMAIL ? contact.email : "" });
    },
  };
}
