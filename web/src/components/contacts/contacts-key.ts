import { createConnectQueryKey } from "@connectrpc/connect-query";

import { ContactsService } from "@/gen/shogun/api/v1/contacts_pb";

// contactsQueryKey matches every cached ListContacts result, whatever the
// filter, so a change refetches the table.
export const contactsQueryKey = createConnectQueryKey({
  schema: ContactsService.method.listContacts,
  cardinality: "infinite",
});
