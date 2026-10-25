// The values a user typed into the address, model and key fields, trimmed.
export interface Typed {
  baseUrl: string;
  model: string;
  apiKey: string;
}

// What is stored for the provider being edited: never the key.
export interface Stored {
  baseUrl: string;
  model: string;
}

// Where the form stands on saving.
export type SaveReadiness =
  // Save is allowed.
  | "ready"
  // Something is still empty.
  | "incomplete"
  // Address, model and key are all there but have not passed a test as typed.
  | "needs-test"
  // A saved provider's address or model changed with no key typed, so the new
  // details cannot be tested: the key has to be entered again.
  | "needs-key";

export function trimmed(typed: Typed): Typed {
  return { baseUrl: typed.baseUrl.trim(), model: typed.model.trim(), apiKey: typed.apiKey.trim() };
}

export function sameTyped(a: Typed | null, b: Typed): boolean {
  return a !== null && a.baseUrl === b.baseUrl && a.model === b.model && a.apiKey === b.apiKey;
}

// The address as the server keeps it: no surrounding space, no trailing slash.
function normalizedAddress(address: string): string {
  return address.trim().replace(/\/+$/, "");
}

// canTest says whether Test connection can run: all three fields are filled.
export function canTest(typed: Typed): boolean {
  const t = trimmed(typed);
  return t.baseUrl !== "" && t.model !== "" && t.apiKey !== "";
}

// saveReadiness decides whether Save is allowed. A provider is saved only after
// a passing test of exactly what is being saved; the one exception is editing
// a saved provider's prices or limit, where the address and model are unchanged
// and the stored key is kept.
export function saveReadiness(typed: Typed, tested: Typed | null, stored: Stored | undefined): SaveReadiness {
  const t = trimmed(typed);
  if (t.apiKey === "") {
    if (stored === undefined) {
      return "incomplete";
    }
    const unchanged = normalizedAddress(t.baseUrl) === normalizedAddress(stored.baseUrl) && t.model === stored.model;
    return unchanged ? "ready" : "needs-key";
  }
  if (t.baseUrl === "" || t.model === "") {
    return "incomplete";
  }
  return sameTyped(tested, t) ? "ready" : "needs-test";
}
