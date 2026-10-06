import { Suspense } from "react";

import { MailCallback } from "@/components/mail/mail-callback";

// useSearchParams needs a Suspense boundary so the page can still be built.
export default function MailCallbackPage() {
  return (
    <Suspense fallback={<div aria-busy="true" className="min-h-40 border-b border-ink p-[22px]" />}>
      <MailCallback />
    </Suspense>
  );
}
