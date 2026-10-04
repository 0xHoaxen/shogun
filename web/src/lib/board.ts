import { create } from "@bufbuild/protobuf";

import {
  BoardColumnSchema,
  JobCardSchema,
  type BoardColumn,
  type JobCard,
  type JobStatus,
} from "@/gen/shogun/api/v1/jobs_pb";

// findCard returns the card with the given id and the status column it is in.
export function findCard(
  columns: readonly BoardColumn[],
  id: string,
): { card: JobCard; status: JobStatus } | undefined {
  for (const column of columns) {
    const card = column.jobs.find((job) => job.id === id);
    if (card) {
      return { card, status: column.status };
    }
  }
  return undefined;
}

// moveCard returns new columns with the card moved to the end of the target
// column. The input is not changed; an unknown id or an unchanged status
// returns the columns as they were.
export function moveCard(
  columns: readonly BoardColumn[],
  id: string,
  to: JobStatus,
): readonly BoardColumn[] {
  const found = findCard(columns, id);
  if (!found || found.status === to) {
    return columns;
  }
  const moved = create(JobCardSchema, { ...found.card, status: to });
  return columns.map((column) => {
    if (column.status === found.status) {
      return create(BoardColumnSchema, {
        status: column.status,
        jobs: column.jobs.filter((job) => job.id !== id),
      });
    }
    if (column.status === to) {
      return create(BoardColumnSchema, { status: column.status, jobs: [...column.jobs, moved] });
    }
    return column;
  });
}

// countJobs is the number of cards on the board.
export function countJobs(columns: readonly BoardColumn[]): number {
  return columns.reduce((total, column) => total + column.jobs.length, 0);
}
