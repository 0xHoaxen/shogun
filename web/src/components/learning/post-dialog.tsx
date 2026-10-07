"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";

import { draftsQueueKey } from "@/components/drafts/drafts-key";
import { draftChannelLabel } from "@/components/drafts/labels";
import { learningErrorText } from "@/components/learning/errors";
import { useLearningClient } from "@/components/learning/use-learning-client";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { SelectField } from "@/components/ui/field";
import { DraftChannel } from "@/gen/shogun/api/v1/drafts_pb";

const POST_CHANNELS = [DraftChannel.LINKEDIN, DraftChannel.X] as const;

interface PostDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  activityIds: readonly string[];
}

export function PostDialog({ open, onOpenChange, activityIds }: PostDialogProps) {
  const client = useLearningClient();
  const queryClient = useQueryClient();
  const router = useRouter();
  const [channel, setChannel] = useState<DraftChannel>(DraftChannel.LINKEDIN);

  const generate = useMutation({
    mutationFn: () => client.generateLearningPost({ activityIds: [...activityIds], channel }),
    onSuccess: async (response) => {
      await queryClient.invalidateQueries({ queryKey: draftsQueueKey });
      generate.reset();
      onOpenChange(false);
      router.push(`/drafts/${response.draftId}`);
    },
  });

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!generate.isPending) {
      generate.mutate();
    }
  }

  const count = activityIds.length;
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Write a post"
      description={`Shogun drafts a post about ${count === 1 ? "this activity" : `these ${count} activities`}. It lands in your drafts, and nothing is posted until you approve it.`}
    >
      <form onSubmit={handleSubmit} className="grid gap-4 p-[22px]">
        <SelectField
          label="Where will you post it?"
          value={channel}
          onChange={(event) => setChannel(Number(event.target.value) as DraftChannel)}
        >
          {POST_CHANNELS.map((option) => (
            <option key={option} value={option}>
              {draftChannelLabel(option)}
            </option>
          ))}
        </SelectField>
        {generate.isError ? (
          <p role="alert">{learningErrorText(generate.error, "Couldn't start the post. Try again.")}</p>
        ) : null}
        <div className="flex gap-2">
          <Button type="submit" variant="primary" disabled={generate.isPending}>
            Write the post
          </Button>
          <Button type="button" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
