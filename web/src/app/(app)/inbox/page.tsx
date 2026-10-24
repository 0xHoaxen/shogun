import { DraftsQueue } from "@/components/drafts/drafts-queue";
import { NotificationsScreen } from "@/components/notifications/notifications-screen";

// The v2 Inbox will replace both; until then the approval queue and the
// notifications list share the page.
export default function InboxPage() {
  return (
    <>
      <DraftsQueue />
      <NotificationsScreen />
    </>
  );
}
