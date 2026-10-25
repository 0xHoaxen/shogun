"use client";

import { useInfiniteQuery } from "@connectrpc/connect-query";
import { useEffect } from "react";

import { ContactsService, type Contact } from "@/gen/shogun/api/v1/contacts_pb";

// PAGE_SIZE is the most ListContacts returns in a page.
const PAGE_SIZE = 200;
// MAX_PEOPLE is how many people the screen loads, the same as the import's row
// limit. The relationship chips are counted in the browser, so everyone has to
// be here; past this the screen says it shows the first part only.
export const MAX_PEOPLE = 5000;

export interface People {
  people: readonly Contact[];
  // loading is true until the first page arrives.
  loading: boolean;
  // failed is true when nothing could be loaded.
  failed: boolean;
  // capped is true when there are more people than MAX_PEOPLE.
  capped: boolean;
  retry: () => void;
}

// usePeople loads every person, page by page, up to MAX_PEOPLE.
export function usePeople(): People {
  const query = useInfiniteQuery(
    ContactsService.method.listContacts,
    { pageSize: PAGE_SIZE, pageToken: "" },
    {
      pageParamKey: "pageToken",
      getNextPageParam: (last) => last.nextPageToken || undefined,
    },
  );
  const people = query.data?.pages.flatMap((page) => page.contacts) ?? [];
  const more = query.hasNextPage && !query.isFetchNextPageError;

  const { fetchNextPage, isFetchingNextPage } = query;
  useEffect(() => {
    if (more && !isFetchingNextPage && people.length < MAX_PEOPLE) {
      void fetchNextPage();
    }
  }, [more, isFetchingNextPage, people.length, fetchNextPage]);

  return {
    people: people.slice(0, MAX_PEOPLE),
    loading: query.isPending,
    failed: query.isError && people.length === 0,
    capped: query.hasNextPage && people.length >= MAX_PEOPLE,
    retry: () => void query.refetch(),
  };
}
