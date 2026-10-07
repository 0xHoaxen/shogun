import { ConnectError } from "@connectrpc/connect";

import { reasonOf } from "@/lib/errors";

// dojo, katana and shinobi prefix their validation messages with this; the rest
// is written for the owner.
const INVALID_PREFIX = "invalid input: ";

const REASONS: Record<string, string> = {
  SOURCE_ROBOTS_DISALLOWED: "That site's robots.txt does not allow reading it, so Shogun left it alone.",
  SOURCE_ADDRESS_NOT_PUBLIC: "That address is not a public one, so Shogun will not read it.",
  SOURCE_TOO_LARGE: "That source's answer is too large to read.",
  SOURCE_FETCH_FAILED: "Shogun could not read that source. Check the address and try again.",
  SOURCE_BAD_DOCUMENT: "That source did not give postings in the shape it was set up for. Check its kind and field mapping.",
  SOURCE_INTERNAL: "Something went wrong reading that source. Try again.",
  SOURCE_NOT_FOUND: "That source no longer exists, so the list has been reloaded.",
  SOURCE_KIND_FIXED: "A source's kind cannot change. Add a new source instead.",
  TOO_MANY_SOURCES: "You have the most sources allowed. Disable one you do not need, or edit it.",
  POSTING_NOT_FOUND: "That posting no longer exists, so the list has been reloaded.",
  POSTING_NOT_SAVABLE: "The tracker could not take this posting as a job.",
  TRACKER_UNAVAILABLE: "The job tracker is busy right now. Try again in a moment.",
  UNAVAILABLE: "Shogun is busy right now. Try again in a moment.",
};

// LAST_ERRORS explain the short code a source keeps for its last failed run.
const LAST_ERRORS: Record<string, string> = {
  robots_disallowed: "its robots.txt does not allow reading it",
  address_not_public: "its address is not a public one",
  too_large: "its answer was too large",
  bad_document: "its answer was not in the expected shape",
  fetch_failed: "it could not be reached",
  internal: "something went wrong",
};

// discoveryErrorText says what went wrong in words the owner can act on. It
// switches on the stable reason; only a validation message, which shinobi writes
// for people, is shown as it came.
export function discoveryErrorText(error: unknown, fallback: string): string {
  const reason = reasonOf(error);
  if (reason === "INVALID_ARGUMENT") {
    const message = ConnectError.from(error).rawMessage;
    return message.startsWith(INVALID_PREFIX) ? message.slice(INVALID_PREFIX.length) : fallback;
  }
  return REASONS[reason] ?? fallback;
}

// lastErrorText turns a source's last-error code into a short phrase.
export function lastErrorText(code: string): string {
  return LAST_ERRORS[code] ?? code;
}
