"use client";

import { createClient } from "@connectrpc/connect";
import { useTransport } from "@connectrpc/connect-query";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState, type FormEvent } from "react";

import { contactQueryKey, contactsQueryKey } from "@/components/contacts/contacts-key";
import { RELATIONSHIPS, RELATIONSHIP_NAMES, type Relationship } from "@/components/contacts/relationship";
import { Button } from "@/components/ui/button";
import { TextAreaField, TextField } from "@/components/ui/field";
import { Segmented } from "@/components/ui/segmented";
import { SidePanel } from "@/components/ui/side-panel";
import { useToast } from "@/components/ui/toast";
import { ContactsService } from "@/gen/shogun/api/v1/contacts_pb";
import { reasonOf } from "@/lib/errors";

const IDEMPOTENCY_KEY_HEADER = "Idempotency-Key";

const ERRORS: Record<string, string> = {
  CONTACT_DUPLICATE: "That person is already in People.",
  INVALID_RELATIONSHIP: "Pick one of the types shown.",
  INVALID_EMAIL: "That email does not look right.",
};

interface Form {
  fullName: string;
  email: string;
  role: string;
  companyName: string;
  relationship: Relationship | "";
  howWeMet: string;
  firstNote: string;
}

const EMPTY: Form = { fullName: "", email: "", role: "", companyName: "", relationship: "", howWeMet: "", firstNote: "" };

interface AddPersonPanelProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  // onAdded is called with the new person's id, so the screen can show them.
  onAdded: (id: string) => void;
}

// AddPersonPanel adds someone to People. Only the name is needed; the company is
// created if it is new. Nothing is sent to the person.
export function AddPersonPanel({ open, onOpenChange, onAdded }: AddPersonPanelProps) {
  const transport = useTransport();
  const client = useMemo(() => createClient(ContactsService, transport), [transport]);
  const queryClient = useQueryClient();
  const toast = useToast();
  const [form, setForm] = useState<Form>(EMPTY);
  // One key per intended person: pressing Add again after a failure returns the
  // original instead of adding a second one.
  const [key, setKey] = useState(() => crypto.randomUUID());

  const add = useMutation({
    mutationFn: async (input: Form) => {
      const response = await client.addContact(
        {
          fullName: input.fullName.trim(),
          email: input.email.trim(),
          role: input.role.trim(),
          companyName: input.companyName.trim(),
          relationship: input.relationship,
          howWeMet: input.howWeMet.trim(),
        },
        { headers: { [IDEMPOTENCY_KEY_HEADER]: key } },
      );
      const id = response.contact?.id ?? "";
      let noteSaved = true;
      if (input.firstNote.trim() !== "" && id !== "") {
        try {
          await client.addContactNote({ id, text: input.firstNote.trim() });
        } catch {
          noteSaved = false;
        }
      }
      return { id, noteSaved };
    },
    onSuccess: async ({ id, noteSaved }) => {
      await queryClient.invalidateQueries({ queryKey: contactsQueryKey });
      await queryClient.invalidateQueries({ queryKey: contactQueryKey });
      setForm(EMPTY);
      setKey(crypto.randomUUID());
      add.reset();
      onOpenChange(false);
      if (!noteSaved) {
        toast.show({ text: "Added, but the first note was not saved. Add it from their page." });
      }
      if (id !== "") {
        onAdded(id);
      }
    },
  });

  function set<K extends keyof Form>(field: K, value: Form[K]) {
    setForm((previous) => ({ ...previous, [field]: value }));
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (form.fullName.trim() !== "" && !add.isPending) {
      add.mutate(form);
    }
  }

  return (
    <SidePanel open={open} onOpenChange={onOpenChange} eyebrow="New person">
      <h2 className="h-display text-3xl">Add a person</h2>
      <p className="text-muted-foreground">Only name is required. Shogun links them to roles by company.</p>
      <form onSubmit={submit} className="flex flex-col gap-4">
        <div className="grid grid-cols-2 gap-4 max-[520px]:grid-cols-1">
          <TextField label="Name" required value={form.fullName} onChange={(e) => set("fullName", e.target.value)} />
          <TextField label="Email" type="email" value={form.email} onChange={(e) => set("email", e.target.value)} />
          <TextField label="Role" value={form.role} onChange={(e) => set("role", e.target.value)} />
          <TextField label="Company" value={form.companyName} onChange={(e) => set("companyName", e.target.value)} />
        </div>
        <div className="flex flex-col gap-1.5">
          <span className="label text-[11px] font-medium">Type</span>
          <Segmented
            label="Type"
            value={form.relationship}
            onChange={(value) => set("relationship", value)}
            options={RELATIONSHIPS.map((value) => ({ value, label: RELATIONSHIP_NAMES[value] }))}
          />
        </div>
        <TextField
          label="How you know them"
          placeholder="Ex-colleague, meetup, LinkedIn"
          value={form.howWeMet}
          onChange={(e) => set("howWeMet", e.target.value)}
        />
        <TextAreaField
          label="First note"
          placeholder="What you talked about, what they offered"
          value={form.firstNote}
          onChange={(e) => set("firstNote", e.target.value)}
        />
        <p className="font-mono text-xs text-muted-foreground">Saved to People only. Nothing is sent.</p>
        {add.isError ? (
          <p role="alert">{ERRORS[reasonOf(add.error)] ?? "Couldn't add them. Try again."}</p>
        ) : null}
        <div className="flex justify-between gap-3">
          <Button type="button" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={form.fullName.trim() === "" || add.isPending}>
            Add person
          </Button>
        </div>
      </form>
    </SidePanel>
  );
}
