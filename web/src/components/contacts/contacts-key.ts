import { createConnectQueryKey } from "@connectrpc/connect-query";

import { ContactsService } from "@/gen/shogun/api/v1/contacts_pb";

// contactQueryKey matches every cached GetContact result, so a change to a
// person reloads their page whoever it was.
export const contactQueryKey = createConnectQueryKey({
  schema: ContactsService.method.getContact,
  cardinality: "finite",
});

// contactsQueryKey matches every cached ListContacts result, whatever the
// filter, so a change refetches the table.
export const contactsQueryKey = createConnectQueryKey({
  schema: ContactsService.method.listContacts,
  cardinality: "infinite",
});
