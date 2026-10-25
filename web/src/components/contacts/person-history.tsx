import { eventTimeText } from "@/components/contacts/follow-up";
import { historyText } from "@/components/contacts/relationship";
import type { ContactEvent } from "@/gen/shogun/api/v1/contacts_pb";
import { timestampDate } from "@bufbuild/protobuf/wkt";

interface PersonHistoryProps {
  events: readonly ContactEvent[];
  now: Date;
}

// PersonHistory is the notes and what happened with a person, newest first.
export function PersonHistory({ events, now }: PersonHistoryProps) {
  if (events.length === 0) {
    return <p className="text-muted-foreground">Nothing yet. Add a note below.</p>;
  }
  return (
    <ol aria-label="Notes and history" className="flex flex-col">
      {events.map((event) => (
        <li key={event.id} className="grid grid-cols-[72px_16px_minmax(0,1fr)] items-start gap-3 py-2">
          <span className="font-mono text-xs text-muted-foreground">
            {event.occurredAt ? eventTimeText(timestampDate(event.occurredAt), now) : ""}
          </span>
          <span aria-hidden="true" className="mt-2 size-1.5 justify-self-center rounded-full bg-scheme" />
          <span className="min-w-0 break-words whitespace-pre-wrap">{historyText(event)}</span>
        </li>
      ))}
    </ol>
  );
}
