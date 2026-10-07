"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";

import { ITEM_KINDS, itemKindLabel } from "@/components/learning/labels";
import { learningErrorText } from "@/components/learning/errors";
import { learningItemsKey } from "@/components/learning/learning-key";
import { useLearningClient } from "@/components/learning/use-learning-client";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { SelectField, TextAreaField, TextField } from "@/components/ui/field";
import { ItemKind } from "@/gen/shogun/api/v1/learning_pb";

interface AddItemDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function AddItemDialog({ open, onOpenChange }: AddItemDialogProps) {
  const client = useLearningClient();
  const queryClient = useQueryClient();
  const [title, setTitle] = useState("");
  const [kind, setKind] = useState<ItemKind>(ItemKind.COURSE);
  const [url, setUrl] = useState("");
  const [insight, setInsight] = useState("");

  const add = useMutation({
    mutationFn: () => client.addLearningItem({ title: title.trim(), kind, url: url.trim(), insight: insight.trim() }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: learningItemsKey });
      setTitle("");
      setUrl("");
      setInsight("");
      add.reset();
      onOpenChange(false);
    },
  });

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (title.trim() !== "" && !add.isPending) {
      add.mutate();
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Add item"
      description="Something you are learning or want to: a course, a book, a project or a skill."
    >
      <form onSubmit={handleSubmit} className="grid gap-4 p-[22px]">
        <TextField label="Title" required value={title} onChange={(event) => setTitle(event.target.value)} />
        <SelectField label="Kind" value={kind} onChange={(event) => setKind(Number(event.target.value) as ItemKind)}>
          {ITEM_KINDS.map((option) => (
            <option key={option} value={option}>
              {itemKindLabel(option)}
            </option>
          ))}
        </SelectField>
        <TextField label="Link" type="url" value={url} onChange={(event) => setUrl(event.target.value)} />
        <TextAreaField
          label="What you took away"
          value={insight}
          onChange={(event) => setInsight(event.target.value)}
        />
        {add.isError ? <p role="alert">{learningErrorText(add.error, "Couldn't add the item. Try again.")}</p> : null}
        <div className="flex gap-2">
          <Button type="submit" variant="primary" disabled={title.trim() === "" || add.isPending}>
            Add item
          </Button>
          <Button type="button" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
