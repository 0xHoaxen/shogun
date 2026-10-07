import { DiscoverySourceKind } from "@/gen/shogun/api/v1/discovery_pb";

const KIND_LABELS: Partial<Record<DiscoverySourceKind, string>> = {
  [DiscoverySourceKind.RSS]: "RSS or Atom feed",
  [DiscoverySourceKind.API]: "JSON API",
  [DiscoverySourceKind.FILE]: "Pasted JSON",
};

export const SOURCE_KINDS: readonly DiscoverySourceKind[] = [
  DiscoverySourceKind.RSS,
  DiscoverySourceKind.API,
  DiscoverySourceKind.FILE,
];

export function sourceKindLabel(kind: DiscoverySourceKind): string {
  return KIND_LABELS[kind] ?? "Source";
}

// SCORE_FILTERS are the choices for the lowest score shown; 0 means all.
export const SCORE_FILTERS: readonly { value: number; label: string }[] = [
  { value: 0, label: "All" },
  { value: 0.5, label: "50% and up" },
  { value: 0.7, label: "70% and up" },
  { value: 0.85, label: "85% and up" },
];

// percent writes a score from 0 to 1 as a whole percentage.
export function percent(score: number): string {
  return `${Math.round(score * 100)}%`;
}

// listFromText reads a comma-separated list the owner typed.
export function listFromText(text: string): string[] {
  return text
    .split(",")
    .map((term) => term.trim())
    .filter(Boolean);
}

// textFromList writes a list back for editing.
export function textFromList(list: readonly string[]): string {
  return list.join(", ");
}
