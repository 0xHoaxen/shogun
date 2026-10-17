import type { ReactNode } from "react";

import { SettingsNav } from "@/components/settings/settings-nav";

export default function SettingsLayout({ children }: { children: ReactNode }) {
  return (
    <div className="grid grid-cols-[168px_minmax(0,1fr)] max-[860px]:grid-cols-1">
      <SettingsNav />
      <div className="min-w-0">{children}</div>
    </div>
  );
}
