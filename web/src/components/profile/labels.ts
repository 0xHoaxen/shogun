import { SuggestionState, SuggestionTarget } from "@/gen/shogun/api/v1/profile_pb";

const TARGET_LABELS: Partial<Record<SuggestionTarget, string>> = {
  [SuggestionTarget.RESUME]: "Resume",
  [SuggestionTarget.LINKEDIN]: "LinkedIn",
};

const STATE_LABELS: Partial<Record<SuggestionState, string>> = {
  [SuggestionState.OPEN]: "Open",
  [SuggestionState.ACCEPTED]: "Accepted",
  [SuggestionState.DISMISSED]: "Dismissed",
};

const SECTION_LABELS: Record<string, string> = {
  headline: "Headline",
  about: "About",
  experience: "Experience",
  skills: "Skills",
  projects: "Projects",
};

// STATE_FILTERS are the filters over the suggestions, in the order the owner
// meets them.
export const STATE_FILTERS: readonly SuggestionState[] = [
  SuggestionState.OPEN,
  SuggestionState.ACCEPTED,
  SuggestionState.DISMISSED,
];

export function suggestionTargetLabel(target: SuggestionTarget): string {
  return TARGET_LABELS[target] ?? "Profile";
}

export function suggestionStateLabel(state: SuggestionState): string {
  return STATE_LABELS[state] ?? "Unknown";
}

export function sectionLabel(section: string): string {
  return SECTION_LABELS[section] ?? section;
}
