import Link from "next/link";

import { NAV_SECTIONS } from "@/content/sections";

// TODO(owner): the landing page after sign-in is /dashboard; say if it should be /inbox.
// The v2 Dashboard replaces this page; until then it only points at the sections.
export default function DashboardPage() {
  return (
    <div className="p-[22px]">
      <h1 className="h-display text-5xl">Dashboard</h1>
      <p className="mt-4 text-muted-foreground">Your widgets arrive in a later release. Pick a section to start.</p>
      <ul className="mt-6 flex flex-col gap-2">
        {NAV_SECTIONS.filter((section) => section.href !== "/dashboard").map((section) => (
          <li key={section.href}>
            <Link href={section.href} className="underline">
              {section.label}
            </Link>
          </li>
        ))}
      </ul>
    </div>
  );
}
