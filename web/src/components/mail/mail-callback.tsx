"use client";

import { useMutation } from "@connectrpc/connect-query";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useEffect, useRef, useState } from "react";

import { PageHeader } from "@/components/shell/page-header";
import { MailService } from "@/gen/shogun/api/v1/mail_pb";
import { REASON_INVALID_CODE, REASON_INVALID_STATE, reasonOf } from "@/lib/errors";

const CALLBACK_PATH = "/mail/callback";

function failureMessage(error: unknown): string {
  switch (reasonOf(error)) {
    case REASON_INVALID_STATE:
      return "This sign-in link has expired or is not valid. Start again from Settings.";
    case REASON_INVALID_CODE:
      return "Google refused the sign-in. Start again from Settings.";
    default:
      return "Couldn't connect your mail. Try again from Settings.";
  }
}

// MailCallback is where Google sends the owner back. It finishes the connection
// with the code and state in the address, once, and removes them from the
// address bar so they do not stay in the browser's history.
export function MailCallback() {
  const params = useSearchParams();
  // Read once. Clearing the address bar below changes what useSearchParams
  // reports, and the screen must keep showing the outcome of this request.
  const [{ code, state, denied }] = useState(() => ({
    code: params.get("code") ?? "",
    state: params.get("state") ?? "",
    denied: params.get("error") ?? "",
  }));
  const started = useRef(false);
  const complete = useMutation(MailService.method.completeConnect);

  useEffect(() => {
    if (started.current || denied || !code || !state) {
      return;
    }
    started.current = true; // a code works once, so never submit it twice
    window.history.replaceState(null, "", CALLBACK_PATH);
    complete.mutate({ code, state });
  }, [code, state, denied, complete]);

  let body: React.ReactNode;
  if (denied) {
    body = <p role="alert">Google did not give access, so nothing was connected.</p>;
  } else if (!code || !state) {
    body = <p role="alert">This page is where Google sends you back. Start from Settings.</p>;
  } else if (complete.isError) {
    body = <p role="alert">{failureMessage(complete.error)}</p>;
  } else if (complete.isSuccess) {
    body = <p role="status">Connected {complete.data.address}. Shogun will start reading its mail.</p>;
  } else {
    body = (
      <p role="status" aria-busy="true">
        Connecting your mail…
      </p>
    );
  }

  return (
    <>
      <PageHeader title="Mail." description="Finishing the connection with Google." />
      <section className="grid gap-3.5 border-b border-ink p-[22px]">
        {body}
        <div>
          <Link
            href="/settings/mail"
            className="label inline-flex min-h-9 items-center border border-ink px-3 font-bold hover:bg-ink hover:text-paper"
          >
            Back to mail settings
          </Link>
        </div>
      </section>
    </>
  );
}
