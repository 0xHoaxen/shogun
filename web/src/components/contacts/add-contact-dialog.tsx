"use client";

import { ConnectError, createClient } from "@connectrpc/connect";
import { useTransport } from "@connectrpc/connect-query";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState, type FormEvent } from "react";

import { contactsQueryKey } from "@/components/contacts/contacts-key";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { TextField } from "@/components/ui/field";
import { ContactsService } from "@/gen/shogun/api/v1/contacts_pb";

const IDEMPOTENCY_KEY_HEADER = "Idempotency-Key";

interface ContactForm {
  fullName: string;
  role: string;
  companyName: string;
  email: string;
  linkedinUrl: string;
  preferredChannel: string;
  notes: string;
}

const EMPTY_FORM: ContactForm = {
  fullName: "",
  role: "",
  companyName: "",
  email: "",
  linkedinUrl: "",
  preferredChannel: "",
  notes: "",
};

const FIELDS: readonly { name: keyof ContactForm; label: string; type?: string; required?: boolean }[] = [
  { name: "fullName", label: "Name", required: true },
  { name: "role", label: "Role" },
  { name: "companyName", label: "Company" },
  { name: "email", label: "Email", type: "email" },
  { name: "linkedinUrl", label: "LinkedIn", type: "url" },
  { name: "preferredChannel", label: "Preferred channel" },
  { name: "notes", label: "Notes" },
];

interface AddContactDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function AddContactDialog({ open, onOpenChange }: AddContactDialogProps) {
  const transport = useTransport();
  const client = useMemo(() => createClient(ContactsService, transport), [transport]);
  const queryClient = useQueryClient();
  const [form, setForm] = useState<ContactForm>(EMPTY_FORM);
  // One key per intended contact: a retry after a failure reuses it, so kagami
  // returns the original contact instead of adding a second one.
  const [idempotencyKey, setIdempotencyKey] = useState(() => crypto.randomUUID());

  const add = useMutation({
    mutationFn: (input: ContactForm) =>
      client.addContact(input, { headers: { [IDEMPOTENCY_KEY_HEADER]: idempotencyKey } }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: contactsQueryKey });
      setForm(EMPTY_FORM);
      setIdempotencyKey(crypto.randomUUID());
      add.reset();
      onOpenChange(false);
    },
  });

  const canSubmit = form.fullName.trim() !== "" && !add.isPending;

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (canSubmit) {
      add.mutate(form);
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Add contact"
      description="Starts as not reached. The company is created if it is new."
    >
      <form onSubmit={handleSubmit} className="grid gap-4 p-[22px]">
        {FIELDS.map((field) => (
          <TextField
            key={field.name}
            label={field.label}
            type={field.type}
            required={field.required}
            value={form[field.name]}
            onChange={(event) => {
              const value = event.target.value;
              setForm((previous) => ({ ...previous, [field.name]: value }));
            }}
          />
        ))}
        {add.isError ? (
          <p role="alert" className="border border-ink bg-strip px-3 py-2">
            {ConnectError.from(add.error).rawMessage}
          </p>
        ) : null}
        <div className="flex justify-end gap-2">
          <Button type="button" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={!canSubmit}>
            Add contact
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
