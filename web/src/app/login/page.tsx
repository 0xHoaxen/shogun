import { Rail } from "@/components/shell/rail";
import { buttonVariants } from "@/components/ui/button";

// Messages for the ?error= codes torii's login flow redirects back with.
const ERROR_MESSAGES: Readonly<Record<string, string>> = {
  not_allowed: "This Google account is not allowed to sign in.",
  invalid_state: "The sign-in took too long. Try again.",
  denied: "Sign-in was cancelled.",
  unavailable: "Google sign-in is unavailable. Try again shortly.",
};
const FALLBACK_ERROR = "Sign-in failed. Try again.";

interface LoginPageProps {
  searchParams: Promise<{ error?: string }>;
}

export default async function LoginPage({ searchParams }: LoginPageProps) {
  const { error } = await searchParams;
  const message = error ? (ERROR_MESSAGES[error] ?? FALLBACK_ERROR) : null;

  return (
    <div className="grid min-h-screen grid-cols-[104px_minmax(0,1fr)] border border-ink bg-paper max-[860px]:grid-cols-1">
      <Rail label="SIGN IN" />
      <main className="flex min-w-0 flex-col justify-center gap-6 p-[22px]">
        <h1 className="h-display text-[72px] max-[860px]:text-5xl">
          Nothing leaves without your seal.
        </h1>
        <p className="max-w-[56ch] text-muted-ink">
          Shogun keeps the books on its own. Anything that speaks for you waits
          for your approval.
        </p>
        {message ? (
          <p role="alert" className="border border-ink bg-strip px-3 py-2">
            {message}
          </p>
        ) : null}
        <div>
          {/* A full navigation: torii starts the Google flow and redirects back. */}
          <a href="/auth/login" className={buttonVariants({ variant: "primary" })}>
            Sign in with Google
          </a>
        </div>
      </main>
    </div>
  );
}
