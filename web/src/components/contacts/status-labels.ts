import { ContactStatus } from "@/gen/shogun/api/v1/contacts_pb";

// FILTER_STATUSES are the status filters in pipeline order, after "All".
export const FILTER_STATUSES: readonly ContactStatus[] = [
  ContactStatus.NOT_REACHED,
  ContactStatus.REACHED_OUT,
  ContactStatus.CONVERSATION_STARTED,
  ContactStatus.REPLIED,
  ContactStatus.REFERRAL_ASKED,
];

const LABELS: ReadonlyMap<ContactStatus, string> = new Map([
  [ContactStatus.NOT_REACHED, "Not reached"],
  [ContactStatus.REACHED_OUT, "Reached out"],
  [ContactStatus.CONVERSATION_STARTED, "Conversation started"],
  [ContactStatus.REPLIED, "Replied"],
  [ContactStatus.REFERRAL_ASKED, "Referral asked"],
]);

export function contactStatusLabel(status: ContactStatus): string {
  return LABELS.get(status) ?? "Unknown";
}
