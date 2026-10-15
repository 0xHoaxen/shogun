import type { ReactNode } from "react";
import Link from "next/link";

import { Rail } from "@/components/shell/rail";

// Where privacy and terms questions go. Google's verification reviewers check
// that this address answers.
// TODO(owner): replace with a mailbox you read before submitting verification.
export const LEGAL_CONTACT_EMAIL = "dev.sboy99@gmail.com";

// The date both documents last changed. Bump it with any edit to their text.
export const LEGAL_EFFECTIVE_DATE = "13 October 2026";

export const SITE_URL = "https://shogun.sboy99.xyz";

interface LegalPageProps {
  railLabel: string;
  title: string;
  children: ReactNode;
}

// A public, signed-out page for a legal document: privacy policy or terms.
export function LegalPage({ railLabel, title, children }: LegalPageProps) {
  return (
    <div className="grid min-h-screen grid-cols-[104px_minmax(0,1fr)] border border-ink bg-paper max-[860px]:grid-cols-1">
      <Rail label={railLabel} />
      <main className="flex min-w-0 flex-col gap-6 p-[22px]">
        <header className="flex flex-col gap-2">
          <h1 className="h-display text-5xl">{title}</h1>
          <p className="text-muted-ink">Effective {LEGAL_EFFECTIVE_DATE}</p>
        </header>
        <article className="flex max-w-[72ch] flex-col gap-6 [&_a]:underline [&_h2]:font-display [&_h2]:text-2xl [&_h2]:font-medium [&_li]:ml-5 [&_li]:list-disc [&_section]:flex [&_section]:flex-col [&_section]:gap-2 [&_ul]:flex [&_ul]:flex-col [&_ul]:gap-1">
          {children}
        </article>
        <LegalLinks />
      </main>
    </div>
  );
}

// Links between the legal pages and sign-in, also shown on the login page.
export function LegalLinks() {
  return (
    <nav aria-label="Legal" className="flex gap-4 text-muted-ink">
      <Link href="/privacy" className="underline">
        Privacy Policy
      </Link>
      <Link href="/terms" className="underline">
        Terms of Service
      </Link>
      <Link href="/login" className="underline">
        Sign in
      </Link>
    </nav>
  );
}
