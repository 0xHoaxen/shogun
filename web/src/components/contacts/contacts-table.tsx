"use client";

import { useInfiniteQuery } from "@connectrpc/connect-query";
import { useState } from "react";

import { AddContactDialog } from "@/components/contacts/add-contact-dialog";
import { ImportDialog } from "@/components/contacts/import-dialog";
import { FILTER_STATUSES, contactStatusLabel } from "@/components/contacts/status-labels";
import { PageHeader } from "@/components/shell/page-header";
import { Button } from "@/components/ui/button";
import { ContactStatus, ContactsService } from "@/gen/shogun/api/v1/contacts_pb";
import { cn } from "@/lib/utils";

const PAGE_SIZE = 50;

const HEADINGS = ["Name", "Company", "Status", "Last touch", "Next step"] as const;

interface FilterButtonProps {
  label: string;
  active: boolean;
  onSelect: () => void;
}

function FilterButton({ label, active, onSelect }: FilterButtonProps) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onSelect}
      className={cn(
        "label -mr-px -mb-px inline-flex min-h-9 cursor-pointer items-center border border-ink px-3 font-medium hover:bg-strip",
        active && "bg-ink text-paper hover:bg-ink",
      )}
    >
      {label}
    </button>
  );
}

export function ContactsTable() {
  const [status, setStatus] = useState<ContactStatus>(ContactStatus.UNSPECIFIED);
  const [adding, setAdding] = useState(false);
  const [importing, setImporting] = useState(false);

  const contacts = useInfiniteQuery(
    ContactsService.method.listContacts,
    { status, pageSize: PAGE_SIZE, pageToken: "" },
    {
      pageParamKey: "pageToken",
      getNextPageParam: (last) => last.nextPageToken || undefined,
    },
  );
  const rows = contacts.data?.pages.flatMap((page) => page.contacts) ?? [];

  return (
    <>
      <PageHeader
        title="Your people."
        description="Everyone you are in touch with, by status. Nothing is sent from this table."
      >
        <Button onClick={() => setImporting(true)}>Import CSV</Button>
        <Button variant="primary" onClick={() => setAdding(true)}>
          Add contact
        </Button>
      </PageHeader>

      <div role="group" aria-label="Filter by status" className="flex flex-wrap border-b border-ink p-[22px] pb-[23px]">
        <FilterButton
          label="All"
          active={status === ContactStatus.UNSPECIFIED}
          onSelect={() => setStatus(ContactStatus.UNSPECIFIED)}
        />
        {FILTER_STATUSES.map((filter) => (
          <FilterButton
            key={filter}
            label={contactStatusLabel(filter)}
            active={status === filter}
            onSelect={() => setStatus(filter)}
          />
        ))}
      </div>

      {contacts.isError ? (
        <div className="flex items-center gap-3 border-b border-ink p-[22px]">
          <p role="alert">Couldn&apos;t load your contacts.</p>
          <Button onClick={() => contacts.refetch()}>Retry</Button>
        </div>
      ) : (
        <div className="overflow-x-auto border-b border-ink">
          <table className="w-full min-w-[860px] border-collapse text-left">
            <thead>
              <tr className="label h-9 bg-ink text-paper">
                {HEADINGS.map((heading) => (
                  <th key={heading} className="px-2.5 font-medium">
                    {heading}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((contact) => (
                <tr key={contact.id} className="h-11 border-b border-ink">
                  <td className="px-2.5 py-1.5">
                    <b>{contact.fullName}</b>
                    <br />
                    <span className="text-[11px] text-muted-ink">{contact.role}</span>
                  </td>
                  <td className="px-2.5">{contact.companyName}</td>
                  <td className="px-2.5">
                    <span className="label inline-flex min-h-5 items-center border border-ink px-2 text-[11px]">
                      {contactStatusLabel(contact.status)}
                    </span>
                  </td>
                  <td className="px-2.5">{contact.lastContacted || "-"}</td>
                  <td className="px-2.5">
                    {contact.nextFollowUp ? `Follow up ${contact.nextFollowUp}` : "-"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {contacts.isSuccess && rows.length === 0 ? (
            <p className="p-[22px] text-muted-ink">Nobody in this status yet.</p>
          ) : null}
        </div>
      )}

      {contacts.hasNextPage ? (
        <div className="border-b border-ink p-[22px]">
          <Button disabled={contacts.isFetchingNextPage} onClick={() => contacts.fetchNextPage()}>
            Load more
          </Button>
        </div>
      ) : null}

      <AddContactDialog open={adding} onOpenChange={setAdding} />
      <ImportDialog open={importing} onOpenChange={setImporting} />
    </>
  );
}
