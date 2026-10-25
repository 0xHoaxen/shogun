"use client";

import { createClient } from "@connectrpc/connect";
import { useQuery, useTransport } from "@connectrpc/connect-query";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useMemo, useState, type FormEvent } from "react";

import { contactQueryKey } from "@/components/contacts/contacts-key";
import { lastContactText } from "@/components/contacts/follow-up";
import { PersonHistory } from "@/components/contacts/person-history";
import { initialsOf, RELATIONSHIP_NAMES, relationshipOf } from "@/components/contacts/relationship";
import { useDraftANote } from "@/components/contacts/use-draft-a-note";
import type { MarkContacted } from "@/components/contacts/use-mark-contacted";
import { statusLabel } from "@/components/jobs/status-labels";
import { Button } from "@/components/ui/button";
import { Tag } from "@/components/ui/tag";
import { ContactsService } from "@/gen/shogun/api/v1/contacts_pb";
import { reasonOf } from "@/lib/errors";

const NOTE_ERRORS: Record<string, string> = {
  INVALID_NOTE: "Write something first.",
  NOTE_TOO_LONG: "A note is at most 2,000 characters.",
};

interface PersonDetailProps {
  id: string;
  today: string;
  now: Date;
  marking: MarkContacted;
  onBack: () => void;
}

// PersonDetail is one person: how to reach them, the roles at their company,
// the notes and history, and what to do next.
export function PersonDetail({ id, today, now, marking, onBack }: PersonDetailProps) {
  const person = useQuery(ContactsService.method.getContact, { id });
  const transport = useTransport();
  const client = useMemo(() => createClient(ContactsService, transport), [transport]);
  const queryClient = useQueryClient();
  const [text, setText] = useState("");
  const draft = useDraftANote();

  const addNote = useMutation({
    mutationFn: (note: string) => client.addContactNote({ id, text: note }),
    onSuccess: async () => {
      setText("");
      await queryClient.invalidateQueries({ queryKey: contactQueryKey });
    },
  });

  if (person.isError) {
    return (
      <div className="flex items-center gap-3 p-7">
        <p role="alert">Couldn&apos;t load this person.</p>
        <Button onClick={() => person.refetch()}>Retry</Button>
      </div>
    );
  }
  if (!person.data?.contact) {
    return <p className="p-7 text-muted-foreground">Loading…</p>;
  }

  const contact = person.data.contact;
  const marked = marking.pending?.id === id ? marking.pending.on : "";
  const lastContact = lastContactText(marked || contact.lastContacted, today);
  const relationship = contact.relationship === "" ? "" : RELATIONSHIP_NAMES[relationshipOf(contact.relationship)];
  const known = [relationship, contact.howWeMet].filter(Boolean).join(" · ");

  function submitNote(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (text.trim() !== "" && !addNote.isPending) {
      addNote.mutate(text.trim());
    }
  }

  return (
    <article aria-label={contact.fullName} className="flex flex-col gap-6 p-7 max-[860px]:p-4">
      <Button className="self-start min-[861px]:hidden" onClick={onBack}>
        Back to people
      </Button>
      <header className="flex items-center gap-4">
        <span
          aria-hidden="true"
          className="label inline-flex size-14 shrink-0 items-center justify-center border border-line2 text-scheme"
        >
          {initialsOf(contact.fullName)}
        </span>
        <div className="min-w-0">
          <h2 className="h-display truncate text-3xl">{contact.fullName}</h2>
          <p className="text-muted-foreground">{[contact.role, contact.companyName].filter(Boolean).join(" · ")}</p>
        </div>
      </header>

      <dl className="grid grid-cols-3 border border-dashed border-line2 max-[860px]:grid-cols-1">
        <div className="min-w-0 p-4">
          <dt className="label text-muted-foreground">Email</dt>
          <dd className="mt-2 font-mono text-sm break-all">{contact.email || "Not set"}</dd>
        </div>
        <div className="min-w-0 border-l border-line2 p-4 max-[860px]:border-t max-[860px]:border-l-0">
          <dt className="label text-muted-foreground">Last contact</dt>
          <dd className="mt-2 font-mono text-sm" data-testid="last-contact">
            {lastContact}
          </dd>
        </div>
        <div className="min-w-0 border-l border-line2 p-4 max-[860px]:border-t max-[860px]:border-l-0">
          <dt className="label text-muted-foreground">How you know them</dt>
          <dd className="mt-2 font-mono text-sm">{known || "Not set"}</dd>
        </div>
      </dl>

      <section aria-labelledby="linked-roles" className="flex flex-col gap-3">
        <h3 id="linked-roles" className="label text-muted-foreground">
          Linked roles
        </h3>
        {person.data.linkedJobs.length === 0 ? (
          <p className="text-muted-foreground">No roles tracked at {contact.companyName || "their company"}.</p>
        ) : (
          <ul className="border border-dashed border-line2">
            {person.data.linkedJobs.map((job) => (
              <li key={job.id} className="flex flex-wrap items-center gap-3 border-b border-line2 px-4 py-3 last:border-b-0">
                <span>{job.title}</span>
                <Tag>{statusLabel(job.status)}</Tag>
                <span className="ml-auto font-mono text-xs text-muted-foreground">{job.companyName}</span>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section aria-labelledby="notes-heading" className="flex flex-col gap-3">
        <h3 id="notes-heading" className="label text-muted-foreground">
          Notes and history
        </h3>
        <PersonHistory events={person.data.history} now={now} />
        <form onSubmit={submitNote} className="flex gap-3 max-[860px]:flex-col">
          <label htmlFor="new-note" className="sr-only">
            Add a note
          </label>
          <input
            id="new-note"
            value={text}
            onChange={(event) => setText(event.target.value)}
            placeholder={'Add a note, like "Met at GopherCon"'}
            autoComplete="off"
            className="min-h-10 min-w-0 grow border border-line2 bg-bg px-2.5 py-2 text-text"
          />
          <Button type="submit" disabled={text.trim() === "" || addNote.isPending}>
            Add note
          </Button>
        </form>
        {addNote.isError ? (
          <p role="alert">{NOTE_ERRORS[reasonOf(addNote.error)] ?? "Couldn't add the note. Try again."}</p>
        ) : null}
      </section>

      <footer className="flex flex-col gap-3">
        <p className="text-muted-foreground">
          Shogun drafts a short note; it waits in Inbox for you. Nothing is sent until you approve it.
        </p>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <Button
            disabled={marked !== ""}
            onClick={() => marking.mark({ id, fullName: contact.fullName, version: contact.version }, today)}
          >
            {marked !== "" ? "Marked contacted" : "Mark contacted"}
          </Button>
          <Button
            variant="primary"
            disabled={draft.pending}
            onClick={() => {
              marking.flush();
              draft.start(contact);
            }}
          >
            Draft a note
          </Button>
        </div>
        {marking.error ? <p role="alert">{marking.error}</p> : null}
        {draft.done ? (
          <p role="status">
            A draft is waiting in{" "}
            <Link href="/inbox" className="underline">
              Inbox
            </Link>
            .
          </p>
        ) : null}
        {draft.problem ? <p role="alert">{draft.problem}</p> : null}
        {draft.needsProvider ? (
          <p role="alert">
            Set up an AI provider to use AI features.{" "}
            <Link href="/settings/provider" className="underline">
              Add it in Settings
            </Link>
          </p>
        ) : null}
      </footer>
    </article>
  );
}

