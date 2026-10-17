import { NotificationSettingsForm } from "@/components/notifications/notification-settings";
import { PageHeader } from "@/components/shell/page-header";
import { SECTIONS } from "@/content/sections";

export default function NotificationSettingsPage() {
  return (
    <>
      <PageHeader about={SECTIONS.alerts}
        title="How Shogun reaches you."
        description="Notifications stay in the app. Choose whether they are on and when the daily digest should stay quiet."
      />
      <NotificationSettingsForm />
    </>
  );
}
