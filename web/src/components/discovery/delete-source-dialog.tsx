"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";

import { postingsKey, sourcesKey } from "@/components/discovery/discovery-key";
import { discoveryErrorText } from "@/components/discovery/errors";
import { useDiscoveryClient } from "@/components/discovery/use-discovery-client";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import type { DiscoverySource } from "@/gen/shogun/api/v1/discovery_pb";

interface DeleteSourceDialogProps {
  source: DiscoverySource;
  onOpenChange: (open: boolean) => void;
  onDeleted: () => void;
}

// Asks before a source goes, since its postings go with it. Jobs already saved
// to the tracker stay there.
export function DeleteSourceDialog({ source, onOpenChange, onDeleted }: DeleteSourceDialogProps) {
  const client = useDiscoveryClient();
  const queryClient = useQueryClient();

  const remove = useMutation({
    mutationFn: () => client.deleteDiscoverySource({ id: source.id }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: sourcesKey });
      await queryClient.invalidateQueries({ queryKey: postingsKey });
      onDeleted();
    },
  });

  return (
    <Dialog
      open
      onOpenChange={onOpenChange}
      title={`Delete ${source.name}?`}
      description="The postings it found go with it. Jobs you already saved to the tracker stay there."
    >
      <div className="grid gap-4 p-[22px]">
        {remove.isError ? (
          <p role="alert">{discoveryErrorText(remove.error, "Couldn't delete the source. Try again.")}</p>
        ) : null}
        <div className="flex gap-2">
          <Button variant="primary" disabled={remove.isPending} onClick={() => remove.mutate()}>
            Delete source
          </Button>
          <Button onClick={() => onOpenChange(false)}>Keep it</Button>
        </div>
      </div>
    </Dialog>
  );
}
