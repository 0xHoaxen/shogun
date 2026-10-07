import { reasonOf } from "@/lib/errors";

const REASONS: Record<string, string> = {
  INVALID_RANGE: "That range does not work. Check the dates: the start must not be after the end, and the range is at most a year.",
  UNAVAILABLE: "Shogun is busy right now. Try again in a moment.",
};

// insightsErrorText says what went wrong in words the owner can act on.
export function insightsErrorText(error: unknown, fallback: string): string {
  return REASONS[reasonOf(error)] ?? fallback;
}
