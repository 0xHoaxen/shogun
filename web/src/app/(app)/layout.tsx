import type { ReactNode } from "react";

import { AppShell } from "@/components/shell/app-shell";

interface AppLayoutProps {
  children: ReactNode;
}

// Every route in this group sits behind the session gate.
export default function AppLayout({ children }: AppLayoutProps) {
  return <AppShell>{children}</AppShell>;
}
