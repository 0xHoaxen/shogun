import { reasonOf } from "@/lib/errors";

const REASONS: Record<string, string> = {
  SUGGESTION_ALREADY_DECIDED: "That suggestion was already decided, so the list has been reloaded.",
  SUGGESTION_NOT_FOUND: "That suggestion no longer exists, so the list has been reloaded.",
  GITHUB_NOT_CONFIGURED: "Connecting GitHub is not set up on this Shogun yet.",
  GITHUB_NOT_CONNECTED: "Your GitHub account is not connected yet, so there is nothing to sync. Connect it in Settings.",
  GITHUB_UNAUTHORIZED: "GitHub refused the connection. Reconnect your GitHub account and try again.",
  GITHUB_RATE_LIMITED: "GitHub's rate limit is used up. Try again later.",
  UNAVAILABLE: "Shogun is busy right now. Try again in a moment.",
};

// profileErrorText says what went wrong in words the owner can act on. It
// switches on the stable reason and never shows a server message.
export function profileErrorText(error: unknown, fallback: string): string {
  return REASONS[reasonOf(error)] ?? fallback;
}
