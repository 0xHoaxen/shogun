import { ConnectError } from "@connectrpc/connect";

import { reasonOf } from "@/lib/errors";

// What each reason an AI-shaped import can fail with means to the owner. Each
// points back at the template, which is shown alongside.
const REASONS: Record<string, string> = {
  IMPORT_NO_NAME_COLUMN:
    "Your AI provider found no column with people's names in this sheet. Add one, or use the template below.",
  IMPORT_RESHAPE_FAILED:
    "Your AI provider could not match this sheet's columns. Try again, or use the template below.",
  AI_PROVIDER_NOT_CONFIGURED:
    "Add your AI provider in Settings to import any sheet, or use the template below.",
  BUDGET_EXHAUSTED: "Your AI budget is used up for now. Try again later, or use the template below.",
  UNAVAILABLE: "Shogun is busy right now. Try again in a moment, or use the template below.",
};

// importErrorText says why an import failed. AI failures get words the owner
// can act on; anything else keeps kagami's own message, which names the row
// or column at fault.
export function importErrorText(error: unknown): string {
  return REASONS[reasonOf(error)] ?? ConnectError.from(error).rawMessage;
}
