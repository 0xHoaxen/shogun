import { ProviderFailure } from "@/gen/shogun/api/v1/providers_pb";
import { reasonOf } from "@/lib/errors";

// What each stable reason means to the person filling in the form.
const REASONS: Record<string, string> = {
  INVALID_PROVIDER_URL:
    "That address will not work. Use a public https address such as https://api.anthropic.com, without /v1, and no username, password or query.",
  INVALID_PROVIDER_MODEL: "That model name will not work. Use letters, digits and . _ : / @ + - only.",
  INVALID_API_KEY: "That key does not look right. It must be 8 to 4096 characters with no spaces.",
  API_KEY_REQUIRED: "Enter your API key. It is needed the first time you save.",
  INVALID_PRICE: "Enter both prices, input and output, or leave both empty.",
  INVALID_TOKEN_CAP: "The monthly token limit must be a whole number above 0, or empty for no limit.",
  VERSION_CONFLICT: "Your provider changed elsewhere, so it has been reloaded. Check it and save again.",
  AI_PROVIDER_NOT_CONFIGURED: "Save a provider first, then test it.",
  BUDGET_EXHAUSTED: "Your budget or token limit is used up for now, so the test did not run. Try again later.",
  METERING_UNAVAILABLE: "Cost metering is unavailable right now. Try again in a moment.",
  UNAVAILABLE: "Shogun is busy right now. Try again in a moment.",
};

// providerErrorText says what went wrong in words the user can act on. It
// switches on the stable reason and never shows a server message, which could
// repeat what was typed.
export function providerErrorText(error: unknown, fallback: string): string {
  return REASONS[reasonOf(error)] ?? fallback;
}

// What a provider that was reached but did not work is telling the user.
const FAILURES: Partial<Record<ProviderFailure, string>> = {
  [ProviderFailure.REJECTED]: "The provider refused the key. Check the key, and that it belongs to this address.",
  [ProviderFailure.RATE_LIMITED]: "The provider says you are going too fast or are out of quota. Try again later.",
  [ProviderFailure.NOT_FOUND]:
    "The provider does not know this model, or the address path is wrong. Check the model name, and that the address has no /v1 on the end.",
  [ProviderFailure.UNREACHABLE]: "No answer came from that address. Check it, and try again.",
  [ProviderFailure.NOT_PUBLIC]:
    "That address points inside a private network, which Shogun never calls. Use the provider's public address.",
  [ProviderFailure.PROVIDER_ERROR]: "The provider had an error on its side. Try again later.",
  [ProviderFailure.OTHER]: "The test failed for a reason Shogun could not name. Check the address, model and key.",
};

// failureText says why a provider that was called did not work.
export function failureText(failure: ProviderFailure): string {
  return FAILURES[failure] ?? FAILURES[ProviderFailure.OTHER] ?? "";
}
