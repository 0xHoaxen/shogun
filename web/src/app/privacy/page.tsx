import type { Metadata } from "next";

import { LEGAL_CONTACT_EMAIL, LegalPage, SITE_URL } from "@/components/legal/legal-page";

export const metadata: Metadata = {
  title: "Privacy Policy · Shogun",
};

// Public privacy policy. Google's OAuth verification reads it, so every claim
// here must match what tsubame, torii and pkg/llm actually do.
export default function PrivacyPage() {
  return (
    <LegalPage railLabel="PRIVACY" title="Privacy Policy">
      <section>
        <p>
          Shogun (&ldquo;Shogun&rdquo;, &ldquo;we&rdquo;) is a private, single-user web
          application at <a href={SITE_URL}>{SITE_URL}</a> that helps its owner run a job
          search: tracking applications and contacts, spotting job-related email, and
          drafting messages that are sent only after the owner approves them. Only Google
          accounts on an allow-list set by the owner can sign in. This policy explains what
          data Shogun handles, including data received from Google APIs, and how.
        </p>
      </section>

      <section>
        <h2>Data we access from Google</h2>
        <ul>
          <li>
            <strong>Google Sign-In</strong> (scopes <code>openid</code>, <code>email</code>,{" "}
            <code>profile</code>): your email address and display name, used only to check
            that your account is allowed to sign in and to show who is signed in.
          </li>
          <li>
            <strong>Read Gmail</strong> (scope <code>gmail.readonly</code>): for each message,
            the sender, recipients, subject, date, Google&rsquo;s short snippet, and the
            message, thread and label identifiers. Shogun requests message metadata only; it
            does not download full message bodies or attachments.
          </li>
          <li>
            <strong>Send Gmail</strong> (scope <code>gmail.send</code>): used only to send an
            email you have approved, as the exact version you approved. Shogun cannot modify,
            label or delete anything in your mailbox.
          </li>
        </ul>
      </section>

      <section>
        <h2>How we use it</h2>
        <ul>
          <li>
            To show you the job-related email in your inbox and sort it into classes such as
            application confirmation, interview invite, rejection, offer or recruiter outreach.
          </li>
          <li>To link a message to the job and the contact it belongs to in your tracker.</li>
          <li>To send emails you approve, and to record that they were sent.</li>
        </ul>
        <p>
          Google user data is used only to provide these user-facing features. It is not used
          for advertising, is not sold, and is not used to build profiles for anyone else.
        </p>
      </section>

      <section>
        <h2>AI processing</h2>
        <p>
          When simple rules cannot classify a message, Shogun sends its sender, subject and
          snippet (never a full body) to Anthropic&rsquo;s Claude API to choose a class. The
          request is made only to provide that classification to you. Under Anthropic&rsquo;s
          commercial terms, API inputs and outputs are not used to train its models. Shogun
          does not use Google user data to develop, improve or train generalized AI or
          machine-learning models.
        </p>
      </section>

      <section>
        <h2>Sharing</h2>
        <p>
          We do not sell, rent or share your data. Data leaves Shogun only to the service
          providers needed to run it:
        </p>
        <ul>
          <li>Google, to sign you in and to read and send mail through the Gmail API.</li>
          <li>Anthropic, for the email classification described above.</li>
          <li>Cloudflare, which carries encrypted web traffic to and from the site.</li>
        </ul>
        <p>
          No person reads your mail data other than you, except where needed for security,
          to comply with the law, or with your explicit agreement.
        </p>
      </section>

      <section>
        <h2>Google API Services User Data Policy</h2>
        <p>
          Shogun&rsquo;s use and transfer to any other app of information received from
          Google APIs will adhere to the{" "}
          <a href="https://developers.google.com/terms/api-services-user-data-policy">
            Google API Services User Data Policy
          </a>
          , including the Limited Use requirements.
        </p>
      </section>

      <section>
        <h2>Storage and security</h2>
        <ul>
          <li>
            Data is stored in Shogun&rsquo;s own PostgreSQL database on a server controlled by
            the owner. It is not stored in third-party analytics or advertising systems.
          </li>
          <li>
            Your Gmail refresh token is stored encrypted (envelope encryption) and is used
            only by the mail service.
          </li>
          <li>All traffic uses HTTPS. Logs never contain message content, tokens or email addresses.</li>
          <li>Each internal service can reach only its own part of the database.</li>
        </ul>
      </section>

      <section>
        <h2>Retention and deletion</h2>
        <ul>
          <li>Mail metadata and classifications are kept while your Gmail account is connected.</li>
          <li>
            You can revoke Shogun&rsquo;s access at any time at{" "}
            <a href="https://myaccount.google.com/permissions">
              myaccount.google.com/permissions
            </a>
            . Shogun then stops reading and sending mail.
          </li>
          <li>
            To have your data deleted, email{" "}
            <a href={`mailto:${LEGAL_CONTACT_EMAIL}`}>{LEGAL_CONTACT_EMAIL}</a>. We delete it
            within 30 days, and database backups that still hold it expire within 14 days
            after that.
          </li>
        </ul>
      </section>

      <section>
        <h2>Cookies</h2>
        <p>
          Shogun sets only the cookies needed to sign you in and keep you signed in. It uses
          no analytics, advertising or tracking cookies.
        </p>
      </section>

      <section>
        <h2>Children</h2>
        <p>Shogun is not meant for anyone under 16 and does not knowingly collect their data.</p>
      </section>

      <section>
        <h2>Changes</h2>
        <p>
          If this policy changes, the new version is posted here with a new effective date.
          A change that widens how Google user data is used will ask for your consent first.
        </p>
      </section>

      <section>
        <h2>Contact</h2>
        <p>
          Questions or requests: <a href={`mailto:${LEGAL_CONTACT_EMAIL}`}>{LEGAL_CONTACT_EMAIL}</a>.
        </p>
      </section>
    </LegalPage>
  );
}
