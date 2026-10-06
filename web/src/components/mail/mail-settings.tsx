"use client";

import { ConnectError } from "@connectrpc/connect";
import { useMutation } from "@connectrpc/connect-query";

import { PageHeader } from "@/components/shell/page-header";
import { Button } from "@/components/ui/button";
import { MailService } from "@/gen/shogun/api/v1/mail_pb";

// safeAuthUrl returns url only when it is an https address. The URL comes from
// our own backend, but a page that sends the browser away should never follow
// anything else.
function safeAuthUrl(url: string): string | null {
  try {
    return new URL(url).protocol === "https:" ? url : null;
  } catch {
    return null;
  }
}

export function MailSettings() {
  const connect = useMutation(MailService.method.connectAccount, {
    onSuccess: (response) => {
      const target = safeAuthUrl(response.authUrl);
      if (target) {
        window.location.assign(target);
      }
    },
  });
  const refused = connect.isSuccess && safeAuthUrl(connect.data.authUrl) === null;

  return (
    <>
      <PageHeader
        title="Your mail."
        description="Connect Gmail so Shogun can read your job mail and send the drafts you approve."
      />
      <section aria-labelledby="gmail-heading" className="grid gap-3.5 border-b border-ink p-[22px]">
        <h2 id="gmail-heading" className="label font-bold">
          Gmail
        </h2>
        <p className="max-w-[60ch] text-muted-ink">
          Shogun asks Google for two things: to read your mail, and to send mail you have approved. It
          cannot change or delete anything, and it never sends a draft you have not approved. Connecting
          an address you already connected replaces its access.
        </p>
        {connect.isError ? (
          <p role="alert" className="border border-ink bg-strip px-3 py-2">
            {ConnectError.from(connect.error).rawMessage}
          </p>
        ) : null}
        {refused ? (
          <p role="alert" className="border border-ink bg-strip px-3 py-2">
            Google&apos;s sign-in address could not be trusted, so nothing was opened.
          </p>
        ) : null}
        <div>
          <Button variant="primary" disabled={connect.isPending} onClick={() => connect.mutate({})}>
            Connect Gmail
          </Button>
        </div>
      </section>
    </>
  );
}
