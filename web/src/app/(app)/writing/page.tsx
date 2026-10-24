import Link from "next/link";

import { DraftDetail } from "@/components/drafts/draft-detail";

interface WritingPageProps {
  searchParams: Promise<{ draft?: string }>;
}

// One draft opens as /writing?draft=<id>. The v2 Writing studio will add the
// list of drafts for the no-draft case.
export default async function WritingPage({ searchParams }: WritingPageProps) {
  const { draft } = await searchParams;
  if (draft) {
    return <DraftDetail id={draft} />;
  }
  return (
    <div className="p-[22px]">
      <h1 className="h-display text-5xl">Writing</h1>
      <p className="mt-4 text-muted-foreground">
        Open a draft from the{" "}
        <Link href="/inbox" className="underline">
          Inbox
        </Link>{" "}
        to edit and approve it.
      </p>
    </div>
  );
}
