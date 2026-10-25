import { isFollowUpDue, lastContactText } from "@/components/contacts/follow-up";
import { initialsOf } from "@/components/contacts/relationship";
import { Tag } from "@/components/ui/tag";
import type { Contact } from "@/gen/shogun/api/v1/contacts_pb";
import { cn } from "@/lib/utils";

interface PeopleListProps {
  people: readonly Contact[];
  activeId: string | undefined;
  today: string;
  onSelect: (id: string) => void;
}

// PeopleList is the column of people: who they are, when you last spoke, and
// whether you owe them a message.
export function PeopleList({ people, activeId, today, onSelect }: PeopleListProps) {
  return (
    <ul aria-label="People" className="flex flex-col">
      {people.map((person) => {
        const active = person.id === activeId;
        return (
          <li key={person.id} className="border-b border-line">
            <button
              type="button"
              aria-current={active ? "true" : undefined}
              onClick={() => onSelect(person.id)}
              className={cn(
                "grid w-full cursor-pointer grid-cols-[40px_minmax(0,1fr)_auto] items-start gap-3 border-l-2 border-transparent px-4 py-3.5 text-left hover:bg-panel",
                active && "border-scheme bg-panel",
              )}
            >
              <span
                aria-hidden="true"
                className="label mt-0.5 inline-flex size-10 items-center justify-center border border-line2 text-scheme"
              >
                {initialsOf(person.fullName)}
              </span>
              <span className="flex min-w-0 flex-col gap-1">
                <span className="truncate font-medium">{person.fullName}</span>
                <span className="truncate font-mono text-xs text-muted-foreground">
                  {[person.role, person.companyName].filter(Boolean).join(" · ") || "No role yet"}
                </span>
              </span>
              <span className="flex flex-col items-end gap-1.5">
                <span className="font-mono text-xs text-muted-foreground">{lastContactText(person.lastContacted, today)}</span>
                {isFollowUpDue(person.nextFollowUp, today) ? <Tag tone="warn">Write back</Tag> : null}
              </span>
            </button>
          </li>
        );
      })}
    </ul>
  );
}
