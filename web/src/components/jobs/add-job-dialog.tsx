"use client";

import { ConnectError, createClient } from "@connectrpc/connect";
import { useTransport } from "@connectrpc/connect-query";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState, type FormEvent } from "react";

import { boardQueryKey } from "@/components/jobs/board-key";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { TextField } from "@/components/ui/field";
import { JobsService } from "@/gen/shogun/api/v1/jobs_pb";

const IDEMPOTENCY_KEY_HEADER = "Idempotency-Key";

interface JobForm {
  title: string;
  companyName: string;
  companyDomain: string;
  url: string;
  location: string;
  salaryText: string;
}

const EMPTY_FORM: JobForm = {
  title: "",
  companyName: "",
  companyDomain: "",
  url: "",
  location: "",
  salaryText: "",
};

interface AddJobDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function AddJobDialog({ open, onOpenChange }: AddJobDialogProps) {
  const transport = useTransport();
  const client = useMemo(() => createClient(JobsService, transport), [transport]);
  const queryClient = useQueryClient();
  const [form, setForm] = useState<JobForm>(EMPTY_FORM);
  // One key per intended job: a retry after a failure reuses it, so kagami
  // returns the original job instead of adding a second one.
  const [idempotencyKey, setIdempotencyKey] = useState(() => crypto.randomUUID());

  const add = useMutation({
    mutationFn: (input: JobForm) =>
      client.addJob(input, { headers: { [IDEMPOTENCY_KEY_HEADER]: idempotencyKey } }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: boardQueryKey });
      setForm(EMPTY_FORM);
      setIdempotencyKey(crypto.randomUUID());
      add.reset();
      onOpenChange(false);
    },
  });

  const canSubmit = form.title.trim() !== "" && form.companyName.trim() !== "" && !add.isPending;

  function setField(name: keyof JobForm, value: string) {
    setForm((previous) => ({ ...previous, [name]: value }));
  }

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
      title="Add job"
      description="Saved to the first column. The company is created if it is new."
    >
      <form onSubmit={handleSubmit} className="grid gap-4 p-[22px]">
        <TextField
          label="Title"
          required
          value={form.title}
          onChange={(event) => setField("title", event.target.value)}
        />
        <TextField
          label="Company"
          required
          value={form.companyName}
          onChange={(event) => setField("companyName", event.target.value)}
        />
        <TextField
          label="Company domain"
          value={form.companyDomain}
          onChange={(event) => setField("companyDomain", event.target.value)}
        />
        <TextField
          label="Link"
          type="url"
          value={form.url}
          onChange={(event) => setField("url", event.target.value)}
        />
        <TextField
          label="Location"
          value={form.location}
          onChange={(event) => setField("location", event.target.value)}
        />
        <TextField
          label="Salary"
          value={form.salaryText}
          onChange={(event) => setField("salaryText", event.target.value)}
        />
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
            Add job
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
