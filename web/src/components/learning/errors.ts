import { ConnectError } from "@connectrpc/connect";

import { REASON_VERSION_CONFLICT, reasonOf } from "@/lib/errors";

const REASON_INVALID_ARGUMENT = "INVALID_ARGUMENT";
const REASON_ITEM_INVALID_TRANSITION = "ITEM_STATUS_INVALID_TRANSITION";
const REASON_DRAFTS_UNAVAILABLE = "UNAVAILABLE";
// dojo prefixes its validation messages with this; the rest is written for the owner.
const INVALID_PREFIX = "invalid input: ";

// learningErrorText says what went wrong in words the owner can act on. Reasons
// are switched on; only a validation message, which dojo writes for people, is
// shown as it came.
export function learningErrorText(error: unknown, fallback: string): string {
  switch (reasonOf(error)) {
    case REASON_INVALID_ARGUMENT: {
      const message = ConnectError.from(error).rawMessage;
      return message.startsWith(INVALID_PREFIX) ? message.slice(INVALID_PREFIX.length) : fallback;
    }
    case REASON_VERSION_CONFLICT:
      return "This item changed somewhere else. It has been reloaded, so try again.";
    case REASON_ITEM_INVALID_TRANSITION:
      return "That move is not allowed from the item's current status.";
    case REASON_DRAFTS_UNAVAILABLE:
      return "Shogun is busy right now. Try again in a moment.";
    default:
      return fallback;
  }
}
