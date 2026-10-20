import { UsersTable } from "@/components/admin/users-table";
import { PageHeader } from "@/components/shell/page-header";

export default function UsersPage() {
  return (
    <>
      <PageHeader
        title="Who uses Shogun."
        description="Anyone with a Google account can sign up. Here you disable an account or approve it to connect Gmail. You see accounts only, never anyone's jobs, contacts, mail or drafts."
      />
      <UsersTable />
    </>
  );
}
