import { ConnectError } from "@connectrpc/connect";

import {
  REASON_BODY_HASH_MISMATCH,
  REASON_RECIPIENT_REQUIRED,
  REASON_SEND_FAILED,
  REASON_SEND_REFUSED,
  REASON_SEND_STATUS_UNKNOWN,
  REASON_SEND_UNAVAILABLE,
  REASON_VERSION_CONFLICT,
  reasonOf,
} from "@/lib/errors";

// approveErrorMessage says what became of the draft and the mail after a
// refused or failed approval, keyed by the stable reason torii attaches. The
// answers differ on purpose: after some the draft is back in the queue, and
// after one it is not known whether the mail went out.
export function approveErrorMessage(error: unknown): string {
  switch (reasonOf(error)) {
    case REASON_VERSION_CONFLICT:
    case REASON_BODY_HASH_MISMATCH:
      return "This draft changed since you opened it. Nothing was approved. Review the new text and approve again.";
    case REASON_RECIPIENT_REQUIRED:
      return "This email has no recipient, so it cannot be approved.";
    case REASON_SEND_REFUSED:
      return "The mail could not be sent. Is your mail account connected? The draft is back in the queue.";
    case REASON_SEND_FAILED:
      return "Your mail provider did not send it. The draft is back in the queue; approve it again to retry.";
    case REASON_SEND_UNAVAILABLE:
      return "The mail service is unavailable, so nothing was sent. The draft is back in the queue.";
    case REASON_SEND_STATUS_UNKNOWN:
      return "We could not confirm whether the mail was sent. Check your Sent folder before approving again.";
    default:
      return error instanceof ConnectError ? error.rawMessage : "Something went wrong. Try again.";
  }
}
