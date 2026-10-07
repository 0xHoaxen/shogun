"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";

import { learningErrorText } from "@/components/learning/errors";
import { learningActivitiesKey } from "@/components/learning/learning-key";
import { useLearningClient } from "@/components/learning/use-learning-client";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { SelectField, TextAreaField, TextField } from "@/components/ui/field";
import type { LearningItem } from "@/gen/shogun/api/v1/learning_pb";

interface LogActivityDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  // items are the choices for what the activity belongs to.
  items: readonly LearningItem[];
}

export function LogActivityDialog({ open, onOpenChange, items }: LogActivityDialogProps) {
  const client = useLearningClient();
  const queryClient = useQueryClient();
  const [summary, setSummary] = useState("");
  const [itemId, setItemId] = useState("");
  const [minutes, setMinutes] = useState("");
  const [occurredOn, setOccurredOn] = useState("");
  const [tags, setTags] = useState("");

  const log = useMutation({
    mutationFn: () =>
      client.logLearningActivity({
        itemId,
        summary: summary.trim(),
        minutes: Number(minutes) || 0,
        occurredOn,
        tags: tags.split(",").map((tag) => tag.trim()).filter(Boolean),
      }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: learningActivitiesKey });
      setSummary("");
      setMinutes("");
      setOccurredOn("");
      setTags("");
      log.reset();
      onOpenChange(false);
    },
  });

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (summary.trim() !== "" && !log.isPending) {
      log.mutate();
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Log activity"
      description="What did you learn or do? Shogun drafts a post about it for you to approve."
    >
      <form onSubmit={handleSubmit} className="grid gap-4 p-[22px]">
        <TextAreaField
          label="What did you learn?"
          required
          value={summary}
          onChange={(event) => setSummary(event.target.value)}
        />
        <SelectField label="Part of" value={itemId} onChange={(event) => setItemId(event.target.value)}>
          <option value="">Nothing in particular</option>
          {items.map((item) => (
            <option key={item.id} value={item.id}>
              {item.title}
            </option>
          ))}
        </SelectField>
        <TextField
          label="Minutes"
          type="number"
          min={0}
          max={1440}
          value={minutes}
          onChange={(event) => setMinutes(event.target.value)}
        />
        <TextField label="Day" type="date" value={occurredOn} onChange={(event) => setOccurredOn(event.target.value)} />
        <TextField
          label="Tags, separated by commas"
          value={tags}
          onChange={(event) => setTags(event.target.value)}
        />
        {log.isError ? <p role="alert">{learningErrorText(log.error, "Couldn't log the activity. Try again.")}</p> : null}
        <div className="flex gap-2">
          <Button type="submit" variant="primary" disabled={summary.trim() === "" || log.isPending}>
            Log activity
          </Button>
          <Button type="button" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
