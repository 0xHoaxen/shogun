"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { useState } from "react";

import { AddPersonPanel } from "@/components/contacts/add-person-panel";
import { ImportDialog } from "@/components/contacts/import-dialog";
import { isFollowUpDue } from "@/components/contacts/follow-up";
import { PeopleList } from "@/components/contacts/people-list";
import { PersonDetail } from "@/components/contacts/person-detail";
import {
  countByRelationship,
  RELATIONSHIP_LABELS,
  relationshipOf,
  type Relationship,
} from "@/components/contacts/relationship";
import { useMarkContacted } from "@/components/contacts/use-mark-contacted";
import { MAX_PEOPLE, usePeople } from "@/components/contacts/use-people";
import { PageHeader } from "@/components/shell/page-header";
import { Button } from "@/components/ui/button";
import { FilterChips, type FilterChip } from "@/components/ui/filter-chips";
import { SECTIONS } from "@/content/sections";
import { ownerToday } from "@/lib/days";
import { cn } from "@/lib/utils";

type Filter = "all" | "write-back" | Relationship;

const PERSON_PARAM = "person";

// PeopleScreen is /people: who you know on the left, the chosen person on the
// right. On a phone it shows one at a time, and the chosen person is in the URL
// so Back works.
export function PeopleScreen() {
  const router = useRouter();
  const params = useSearchParams();
  const { people, loading, failed, capped, retry } = usePeople();
  const marking = useMarkContacted();
  const [filter, setFilter] = useState<Filter>("all");
  const [adding, setAdding] = useState(false);
  const [importing, setImporting] = useState(false);
  const [now] = useState(() => new Date());
  const today = ownerToday(now);

  const due = people.filter((person) => isFollowUpDue(person.nextFollowUp, today));
  const counts = countByRelationship(people);
  const chips: FilterChip<Filter>[] = [
    { value: "all", label: "All", count: people.length },
    ...counts.map(({ relationship, count }) => ({ value: relationship, label: RELATIONSHIP_LABELS[relationship], count })),
    { value: "write-back", label: "Write back", count: due.length },
  ];
  // A chip that has emptied out (the last person under it was marked) is not
  // offered any more, so the screen falls back to everyone.
  const active: Filter = chips.some((chip) => chip.value === filter) ? filter : "all";
  const shown =
    active === "all"
      ? people
      : active === "write-back"
        ? due
        : people.filter((person) => relationshipOf(person.relationship) === active);

  const chosen = params.get(PERSON_PARAM) ?? undefined;
  // On a wide screen the first person is shown when none is chosen; on a phone
  // the list comes first and a person only opens when picked.
  const activeId = chosen ?? shown[0]?.id;

  function select(id: string | undefined) {
    marking.flush();
    router.push(id === undefined ? "/people" : `/people?${PERSON_PARAM}=${encodeURIComponent(id)}`);
  }

  return (
    <>
      <PageHeader
        about={SECTIONS.contacts}
        title="Who you know."
        description="The people who matter to your search, and who to write to next. Nothing is sent from here."
      />
      <div className="flex flex-wrap items-center gap-3 border-b border-line px-[22px] py-4">
        <FilterChips label="Filter people" chips={chips} value={active} onChange={setFilter} />
        <div className="flex gap-2 max-[860px]:w-full">
          <Button onClick={() => setImporting(true)}>Import CSV</Button>
          <Button variant="primary" onClick={() => setAdding(true)}>
            Add person
          </Button>
        </div>
      </div>

      {failed ? (
        <div className="flex items-center gap-3 p-[22px]">
          <p role="alert">Couldn&apos;t load your people.</p>
          <Button onClick={retry}>Retry</Button>
        </div>
      ) : loading ? (
        <p className="p-[22px] text-muted-foreground">Loading…</p>
      ) : people.length === 0 ? (
        <div className="flex flex-col items-start gap-3 p-[22px]">
          <p>Nobody here yet. Add someone you know, or import a list.</p>
          <div className="flex gap-2">
            <Button variant="primary" onClick={() => setAdding(true)}>
              Add person
            </Button>
            <Button onClick={() => setImporting(true)}>Import CSV</Button>
          </div>
        </div>
      ) : (
        <div className="grid min-h-[480px] grid-cols-[minmax(0,420px)_minmax(0,1fr)] max-[860px]:grid-cols-1">
          <div className={cn("border-r border-line max-[860px]:border-r-0", chosen !== undefined && "max-[860px]:hidden")}>
            {shown.length === 0 ? (
              <p className="p-[22px] text-muted-foreground">Nobody here right now.</p>
            ) : (
              <PeopleList people={shown} activeId={activeId} today={today} onSelect={select} />
            )}
            {capped ? (
              <p className="border-t border-line p-4 text-muted-foreground">
                Showing the first {MAX_PEOPLE.toLocaleString("en-US")} people.
              </p>
            ) : null}
          </div>
          <div className={cn("min-w-0", chosen === undefined && "max-[860px]:hidden")}>
            {activeId !== undefined ? (
              <PersonDetail key={activeId} id={activeId} today={today} now={now} marking={marking} onBack={() => select(undefined)} />
            ) : null}
          </div>
        </div>
      )}

      <AddPersonPanel open={adding} onOpenChange={setAdding} onAdded={(id) => select(id)} />
      <ImportDialog open={importing} onOpenChange={setImporting} />
    </>
  );
}
