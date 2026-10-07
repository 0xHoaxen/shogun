import { sectionLabel, suggestionStateLabel, suggestionTargetLabel } from "@/components/profile/labels";
import { Button } from "@/components/ui/button";
import { SuggestionState, type ProfileSuggestion } from "@/gen/shogun/api/v1/profile_pb";

interface SuggestionCardProps {
  suggestion: ProfileSuggestion;
  // busy is true while a decision on this suggestion is in flight.
  busy: boolean;
  onAccept: (id: string) => void;
  onDismiss: (id: string) => void;
}

const WEB_LINK = /^https?:\/\//;

export function SuggestionCard({ suggestion, busy, onAccept, onDismiss }: SuggestionCardProps) {
  const open = suggestion.state === SuggestionState.OPEN;
  const target = suggestionTargetLabel(suggestion.target);
  const section = sectionLabel(suggestion.section);
  return (
    <article aria-label={`${target} ${section} suggestion`} className="grid gap-3 border-b border-ink p-[22px]">
      <div className="flex flex-wrap items-center gap-2">
        <span className="label inline-flex min-h-5 items-center border border-ink px-2 text-[11px]">{target}</span>
        <span className="label inline-flex min-h-5 items-center border border-ink px-2 text-[11px]">{section}</span>
        {open ? null : (
          <span className="label inline-flex min-h-5 items-center bg-ink px-2 text-[11px] text-paper">
            {suggestionStateLabel(suggestion.state)}
          </span>
        )}
      </div>
      {suggestion.before ? <p className="text-muted-ink line-through">{suggestion.before}</p> : null}
      <p className="whitespace-pre-wrap">{suggestion.after}</p>
      <p className="text-muted-ink">{suggestion.reason}</p>
      {suggestion.evidence.length > 0 ? (
        <ul aria-label="Evidence" className="flex flex-wrap gap-3 text-[11px]">
          {suggestion.evidence
            .filter((item) => WEB_LINK.test(item.url))
            .map((item) => (
              <li key={item.url}>
                <a href={item.url} target="_blank" rel="noopener noreferrer" className="underline">
                  {item.label}
                </a>
              </li>
            ))}
        </ul>
      ) : null}
      {open ? (
        <div className="flex gap-2">
          <Button variant="primary" disabled={busy} onClick={() => onAccept(suggestion.id)}>
            Accept
          </Button>
          <Button disabled={busy} onClick={() => onDismiss(suggestion.id)}>
            Dismiss
          </Button>
        </div>
      ) : null}
    </article>
  );
}
