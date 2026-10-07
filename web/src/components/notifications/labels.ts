import { NotificationType } from "@/gen/shogun/api/v1/notifications_pb";

const TYPE_LABELS: Partial<Record<NotificationType, string>> = {
  [NotificationType.DRAFT_READY]: "Drafts",
  [NotificationType.DRAFT_FAILED]: "Drafts",
  [NotificationType.DRAFT_SEND_FAILED]: "Drafts",
  [NotificationType.INTERVIEW_INVITE]: "Tracker",
  [NotificationType.OFFER]: "Tracker",
  [NotificationType.REJECTION]: "Tracker",
  [NotificationType.REPLY_DETECTED]: "Contacts",
  [NotificationType.FOLLOW_UP_DUE]: "Follow-up",
  [NotificationType.BUDGET_THRESHOLD]: "Spend",
  [NotificationType.BUDGET_EXHAUSTED]: "Spend",
  [NotificationType.DAILY_DIGEST]: "Digest",
};

// notificationTag is the short area name shown beside a notification.
export function notificationTag(type: NotificationType): string {
  return TYPE_LABELS[type] ?? "Shogun";
}
