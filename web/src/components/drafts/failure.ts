// What the reason stored on a failed draft means to the person reading it. A
// reason this list does not know is shown as a plain failure, not as a code.
const FAILURES: Record<string, string> = {
  provider_not_configured: "you have not added an AI provider yet",
  budget_exhausted: "your budget or monthly token limit is used up for now",
  metering_unavailable: "cost metering was unavailable",
  model_refused: "the model declined to write it",
  empty_draft: "the model returned nothing",
  context_unavailable: "Shogun could not read the job or contact it is about",
  generation_failed: "the AI provider failed",
};

// failureReasonText turns a draft's failure reason into words.
export function failureReasonText(reason: string): string {
  return FAILURES[reason] ?? "";
}

// SETTINGS_HINT_REASONS are the failures the user fixes in Settings.
export const SETTINGS_HINT_REASONS: ReadonlySet<string> = new Set(["provider_not_configured", "generation_failed", "budget_exhausted"]);
