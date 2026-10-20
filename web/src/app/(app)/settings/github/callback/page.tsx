import { Suspense } from "react";

import { GitHubCallback } from "@/components/github/github-callback";

// useSearchParams needs a Suspense boundary so the page can still be built.
export default function GitHubCallbackPage() {
  return (
    <Suspense fallback={<div aria-busy="true" className="min-h-40 border-b border-ink p-[22px]" />}>
      <GitHubCallback />
    </Suspense>
  );
}
