import { DraftChannel, DraftKind, DraftState } from "@/gen/shogun/api/v1/drafts_pb";

const KIND_LABELS: Partial<Record<DraftKind, string>> = {
  [DraftKind.COVER_LETTER]: "Cover letter",
  [DraftKind.OUTREACH]: "Outreach",
  [DraftKind.FOLLOW_UP]: "Follow-up",
  [DraftKind.POST]: "Post",
  [DraftKind.ONE_OFF]: "Message",
};

const CHANNEL_LABELS: Partial<Record<DraftChannel, string>> = {
  [DraftChannel.EMAIL]: "Email",
  [DraftChannel.LINKEDIN]: "LinkedIn",
  [DraftChannel.X]: "X",
  [DraftChannel.OTHER]: "Copy only",
};

const STATE_LABELS: Partial<Record<DraftState, string>> = {
  [DraftState.GENERATING]: "Being written",
  [DraftState.PENDING]: "Needs you",
  [DraftState.APPROVED]: "Approved",
  [DraftState.SENT]: "Sent",
  [DraftState.DISCARDED]: "Discarded",
  [DraftState.FAILED]: "Failed",
};

export function draftKindLabel(kind: DraftKind): string {
  return KIND_LABELS[kind] ?? "Draft";
}

export function draftChannelLabel(channel: DraftChannel): string {
  return CHANNEL_LABELS[channel] ?? "Unknown";
}

export function draftStateLabel(state: DraftState): string {
  return STATE_LABELS[state] ?? "Unknown";
}

// QUEUE_STATES are the filters of the queue, in the order the owner meets them.
export const QUEUE_STATES: readonly DraftState[] = [
  DraftState.PENDING,
  DraftState.GENERATING,
  DraftState.APPROVED,
  DraftState.SENT,
  DraftState.FAILED,
];
