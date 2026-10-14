import type { Metadata } from "next";
import Link from "next/link";

import { LEGAL_CONTACT_EMAIL, LegalPage, SITE_URL } from "@/components/legal/legal-page";

export const metadata: Metadata = {
  title: "Terms of Service · Shogun",
};

// Public terms of service, linked from Google's OAuth consent screen.
export default function TermsPage() {
  return (
    <LegalPage railLabel="TERMS" title="Terms of Service">
      <section>
        <p>
          These terms govern your use of Shogun at <a href={SITE_URL}>{SITE_URL}</a>. By
          signing in you agree to them. If you do not agree, do not use Shogun.
        </p>
      </section>

      <section>
        <h2>The service</h2>
        <p>
          Shogun is a private, single-user application for managing a job search: a job and
          contact tracker, detection of job-related email in a connected Gmail account,
          AI-drafted messages, a learning log and related tools. Access is limited to Google
          accounts the owner has allowed; there is no public sign-up.
        </p>
      </section>

      <section>
        <h2>Your Google account</h2>
        <ul>
          <li>
            You sign in with Google and may connect a Gmail account. You must own that account
            or be authorised to use it.
          </li>
          <li>
            Shogun reads message metadata to find job-related mail and sends only emails you
            approve. Details are in the <Link href="/privacy">Privacy Policy</Link>.
          </li>
          <li>
            You can disconnect at any time at{" "}
            <a href="https://myaccount.google.com/permissions">
              myaccount.google.com/permissions
            </a>
            .
          </li>
        </ul>
      </section>

      <section>
        <h2>Nothing is sent without your approval</h2>
        <p>
          Shogun never sends an email, message or post on your behalf unless you approve that
          exact draft. You are responsible for the content of every message you approve and
          for following the law and the rules of any service you send through.
        </p>
      </section>

      <section>
        <h2>AI-generated content</h2>
        <p>
          Drafts and email classifications are produced with AI models and can be wrong,
          incomplete or unsuitable. Review every draft before approving it, and do not rely on
          a classification without checking the message itself.
        </p>
      </section>

      <section>
        <h2>Acceptable use</h2>
        <p>Do not use Shogun to:</p>
        <ul>
          <li>send spam, bulk unsolicited messages, or misleading or unlawful content;</li>
          <li>access accounts or data you are not authorised to access;</li>
          <li>interfere with, probe or overload the service or the APIs it uses.</li>
        </ul>
      </section>

      <section>
        <h2>Third-party services</h2>
        <p>
          Shogun relies on Google, Anthropic and Cloudflare. Your use of Google services is
          also governed by Google&rsquo;s own terms. Shogun is not responsible for those
          services or their availability.
        </p>
      </section>

      <section>
        <h2>No warranty</h2>
        <p>
          Shogun is provided &ldquo;as is&rdquo; and &ldquo;as available&rdquo;, without
          warranties of any kind, including fitness for a particular purpose. It may change,
          pause or stop at any time.
        </p>
      </section>

      <section>
        <h2>Limitation of liability</h2>
        <p>
          To the fullest extent the law allows, the operator of Shogun is not liable for any
          indirect, incidental or consequential loss, or for loss of data, opportunities or
          income, arising from your use of Shogun.
        </p>
      </section>

      <section>
        <h2>Ending access</h2>
        <p>
          You may stop using Shogun and revoke its Google access at any time. Access may be
          removed if these terms are broken. Your data is then handled as described in the{" "}
          <Link href="/privacy">Privacy Policy</Link>.
        </p>
      </section>

      <section>
        <h2>Changes</h2>
        <p>
          These terms may be updated; the new version is posted here with a new effective
          date. Continuing to use Shogun after a change means you accept it.
        </p>
      </section>

      <section>
        <h2>Contact</h2>
        <p>
          Questions: <a href={`mailto:${LEGAL_CONTACT_EMAIL}`}>{LEGAL_CONTACT_EMAIL}</a>.
        </p>
      </section>
    </LegalPage>
  );
}
