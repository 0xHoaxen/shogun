"use client";

import { create } from "@bufbuild/protobuf";
import { createClient } from "@connectrpc/connect";
import { useQuery, useTransport } from "@connectrpc/connect-query";
import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  useDraggable,
  useDroppable,
  useSensor,
  useSensors,
  type DragEndEvent,
  type KeyboardCoordinateGetter,
} from "@dnd-kit/core";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";

import { AddJobDialog } from "@/components/jobs/add-job-dialog";
import { boardQueryKey } from "@/components/jobs/board-key";
import { statusLabel } from "@/components/jobs/status-labels";
import { PageHeader } from "@/components/shell/page-header";
import { Button } from "@/components/ui/button";
import {
  GetBoardResponseSchema,
  JobsService,
  type BoardColumn,
  type GetBoardResponse,
  type JobCard,
  type JobStatus,
} from "@/gen/shogun/api/v1/jobs_pb";
import { countJobs, findCard, moveCard } from "@/lib/board";
import { REASON_JOB_INVALID_TRANSITION, REASON_VERSION_CONFLICT, reasonOf } from "@/lib/errors";
import { cn } from "@/lib/utils";

const DRAG_START_DISTANCE_PX = 8;

interface MoveVariables {
  id: string;
  from: JobStatus;
  to: JobStatus;
  version: number;
}

function moveNotice(error: unknown, from: JobStatus, to: JobStatus): string {
  switch (reasonOf(error)) {
    case REASON_JOB_INVALID_TRANSITION:
      return `A job can't move from ${statusLabel(from)} to ${statusLabel(to)}.`;
    case REASON_VERSION_CONFLICT:
      return "This job changed elsewhere. The board was reloaded.";
    default:
      return "Couldn't move the job. Try again.";
  }
}

// Left and right arrows carry the card being dragged to the neighbouring
// column; dnd-kit's default would nudge it 25px at a time.
const columnKeyboardCoordinates: KeyboardCoordinateGetter = (
  event,
  { context: { droppableRects, droppableContainers, collisionRect }, currentCoordinates },
) => {
  if ((event.code !== "ArrowRight" && event.code !== "ArrowLeft") || !collisionRect) {
    return undefined;
  }
  event.preventDefault();
  const rects = droppableContainers
    .getEnabled()
    .map((container) => droppableRects.get(container.id))
    .filter((rect) => rect !== undefined)
    .sort((a, b) => a.left - b.left);
  const centerX = collisionRect.left + collisionRect.width / 2;
  const index = rects.findIndex((rect) => centerX >= rect.left && centerX < rect.left + rect.width);
  const target = rects[index + (event.code === "ArrowRight" ? 1 : -1)];
  if (index < 0 || !target) {
    return undefined;
  }
  return { x: target.left + (target.width - collisionRect.width) / 2, y: currentCoordinates.y };
};

function Card({ card }: { card: JobCard }) {
  const { attributes, listeners, setNodeRef, transform, isDragging } = useDraggable({ id: card.id });
  return (
    <div
      ref={setNodeRef}
      {...attributes}
      {...listeners}
      style={transform ? { transform: `translate3d(${transform.x}px, ${transform.y}px, 0)` } : undefined}
      className={cn(
        "touch-none cursor-grab border-b border-ink bg-paper px-3 py-2.5 text-left hover:bg-strip",
        isDragging && "relative z-10 bg-signal",
      )}
    >
      <div className="font-bold">{card.title}</div>
      <div className="text-muted-ink">{card.companyName}</div>
      {card.nextFollowUp ? (
        <div className="label mt-1.5 text-[11px] font-bold">Follow up {card.nextFollowUp}</div>
      ) : null}
    </div>
  );
}

function Column({ column }: { column: BoardColumn }) {
  const { setNodeRef, isOver } = useDroppable({
    id: `column-${column.status}`,
    data: { status: column.status },
  });
  const name = statusLabel(column.status);
  return (
    <section
      ref={setNodeRef}
      aria-label={name}
      className={cn("min-h-[380px] min-w-0 border-r border-ink last:border-r-0", isOver && "bg-strip")}
    >
      <div className="label flex min-h-9 items-center justify-between bg-ink px-3 font-medium text-paper">
        <span>{name}</span>
        <span>{column.jobs.length}</span>
      </div>
      {column.jobs.map((card) => (
        <Card key={card.id} card={card} />
      ))}
    </section>
  );
}

export function JobsBoard() {
  const transport = useTransport();
  const client = useMemo(() => createClient(JobsService, transport), [transport]);
  const queryClient = useQueryClient();
  const board = useQuery(JobsService.method.getBoard);
  const [notice, setNotice] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: DRAG_START_DISTANCE_PX } }),
    useSensor(KeyboardSensor, { coordinateGetter: columnKeyboardCoordinates }),
  );

  const move = useMutation({
    mutationFn: (vars: MoveVariables) =>
      client.changeJobStatus({ id: vars.id, toStatus: vars.to, version: vars.version }),
    onMutate: async (vars) => {
      await queryClient.cancelQueries({ queryKey: boardQueryKey });
      const previous = queryClient.getQueriesData<GetBoardResponse>({ queryKey: boardQueryKey });
      queryClient.setQueriesData<GetBoardResponse>({ queryKey: boardQueryKey }, (old) =>
        old
          ? create(GetBoardResponseSchema, { columns: [...moveCard(old.columns, vars.id, vars.to)] })
          : old,
      );
      return { previous };
    },
    onError: (error, vars, context) => {
      for (const [key, data] of context?.previous ?? []) {
        queryClient.setQueryData(key, data);
      }
      setNotice(moveNotice(error, vars.from, vars.to));
    },
    // The server's copy has the new version numbers, so always reload.
    onSettled: () => queryClient.invalidateQueries({ queryKey: boardQueryKey }),
  });

  const columns = board.data?.columns ?? [];

  function handleDragEnd({ active, over }: DragEndEvent) {
    const to = (over?.data.current as { status?: JobStatus } | undefined)?.status;
    const found = findCard(columns, String(active.id));
    if (to === undefined || !found || found.status === to) {
      return;
    }
    move.mutate({ id: found.card.id, from: found.status, to, version: found.card.version });
  }

  const total = countJobs(columns);
  const title = board.data
    ? `${total} ${total === 1 ? "role" : "roles"}, six stages.`
    : "Your pipeline.";

  return (
    <>
      <PageHeader
        title={title}
        description="Drag a card to another stage. Each move is logged on the job."
      >
        <Button variant="primary" onClick={() => setAdding(true)}>
          Add job
        </Button>
      </PageHeader>
      {notice ? (
        <p role="alert" className="border-b border-ink bg-strip px-[22px] py-2">
          {notice}
        </p>
      ) : null}
      {board.isError ? (
        <div className="flex items-center gap-3 border-b border-ink p-[22px]">
          <p role="alert">Couldn&apos;t load the board.</p>
          <Button onClick={() => board.refetch()}>Retry</Button>
        </div>
      ) : (
        <DndContext sensors={sensors} onDragStart={() => setNotice(null)} onDragEnd={handleDragEnd}>
          <div className="overflow-x-auto border-b border-ink">
            <div className="grid min-w-[1010px] grid-cols-[repeat(6,minmax(168px,1fr))]">
              {columns.map((column) => (
                <Column key={column.status} column={column} />
              ))}
            </div>
          </div>
        </DndContext>
      )}
      <AddJobDialog open={adding} onOpenChange={setAdding} />
    </>
  );
}
