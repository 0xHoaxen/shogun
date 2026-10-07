import { reasonOf } from "@/lib/errors";

const REASONS: Record<string, string> = {
  SUGGESTION_ALREADY_DECIDED: "That suggestion was already decided, so the list has been reloaded.",
  SUGGESTION_NOT_FOUND: "That suggestion no longer exists, so the list has been reloaded.",
  GITHUB_NOT_CONFIGURED: "GitHub is not connected. Set KATANA_GITHUB_USER and KATANA_GITHUB_TOKEN, then try again.",
  GITHUB_UNAUTHORIZED: "GitHub refused the token. Check KATANA_GITHUB_TOKEN, then try again.",
  GITHUB_RATE_LIMITED: "GitHub's rate limit is used up. Try again later.",
  UNAVAILABLE: "Shogun is busy right now. Try again in a moment.",
};

// profileErrorText says what went wrong in words the owner can act on. It
// switches on the stable reason and never shows a server message.
export function profileErrorText(error: unknown, fallback: string): string {
  return REASONS[reasonOf(error)] ?? fallback;
}
