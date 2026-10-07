import { NotificationSettingsForm } from "@/components/notifications/notification-settings";
import { PageHeader } from "@/components/shell/page-header";

export default function NotificationSettingsPage() {
  return (
    <>
      <PageHeader
        title="How Shogun reaches you."
        description="Notifications stay in the app. Choose whether they are on and when the daily digest should stay quiet."
      />
      <NotificationSettingsForm />
    </>
  );
}
