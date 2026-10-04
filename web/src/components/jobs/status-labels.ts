import { JobStatus } from "@/gen/shogun/api/v1/jobs_pb";

// STATUS_LABELS are the column names, in pipeline order.
export const STATUS_LABELS: ReadonlyMap<JobStatus, string> = new Map([
  [JobStatus.SAVED, "Saved"],
  [JobStatus.APPLIED, "Applied"],
  [JobStatus.SHORTLISTED, "Shortlisted"],
  [JobStatus.INTERVIEW, "Interview"],
  [JobStatus.OFFER, "Offer"],
  [JobStatus.REJECTED, "Rejected"],
]);

export function statusLabel(status: JobStatus): string {
  return STATUS_LABELS.get(status) ?? "Unknown";
}
