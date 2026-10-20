"use client";

import { createConnectQueryKey, useMutation, useQuery } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { Button } from "@/components/ui/button";
import { AdminService, UserRole, UserStatus, type User } from "@/gen/shogun/api/v1/admin_pb";
import { AuthService } from "@/gen/shogun/api/v1/auth_pb";
import { reasonOf } from "@/lib/errors";

const PAGE_SIZE = 50;
const REASON_ADMIN_CANNOT_DISABLE_SELF = "ADMIN_CANNOT_DISABLE_SELF";

const usersQueryKey = createConnectQueryKey({
  schema: AdminService.method.listUsers,
  cardinality: "finite",
});

function joined(user: User): string {
  return user.createdAt ? new Date(Number(user.createdAt.seconds) * 1000).toISOString().slice(0, 10) : "-";
}

interface UserRowProps {
  user: User;
  isSelf: boolean;
}

// UserRow shows one account with the two things an admin may change: whether it
// can sign in, and whether it may connect Gmail.
function UserRow({ user, isSelf }: UserRowProps) {
  const queryClient = useQueryClient();
  const refresh = () => queryClient.invalidateQueries({ queryKey: usersQueryKey });
  const setStatus = useMutation(AdminService.method.setUserStatus, { onSuccess: refresh });
  const setGmail = useMutation(AdminService.method.setGmailAllowed, { onSuccess: refresh });
  const disabled = user.status === UserStatus.DISABLED;
  const busy = setStatus.isPending || setGmail.isPending;
  const failure = setStatus.error ?? setGmail.error;

  return (
    <>
      <tr className="h-11 border-b border-ink">
        <td className="px-2.5">{user.email}</td>
        <td className="px-2.5">{user.displayName || "-"}</td>
        <td className="px-2.5">{user.role === UserRole.ADMIN ? "Admin" : "Member"}</td>
        <td className="px-2.5">{disabled ? "Disabled" : "Active"}</td>
        <td className="px-2.5">{user.gmailAllowed ? "Approved" : "Not approved"}</td>
        <td className="px-2.5">{joined(user)}</td>
        <td className="px-2.5">
          <div className="flex justify-end gap-2">
            <Button
              disabled={busy}
              aria-label={`${user.gmailAllowed ? "Withdraw Gmail from" : "Approve Gmail for"} ${user.email}`}
              onClick={() => setGmail.mutate({ id: user.id, gmailAllowed: !user.gmailAllowed })}
            >
              {user.gmailAllowed ? "Withdraw Gmail" : "Approve Gmail"}
            </Button>
            <Button
              disabled={busy || (isSelf && !disabled)}
              aria-label={`${disabled ? "Enable" : "Disable"} ${user.email}`}
              onClick={() => setStatus.mutate({ id: user.id, status: disabled ? UserStatus.ACTIVE : UserStatus.DISABLED })}
            >
              {disabled ? "Enable" : "Disable"}
            </Button>
          </div>
        </td>
      </tr>
      {failure ? (
        <tr className="border-b border-ink">
          <td colSpan={7} role="alert" className="px-2.5 py-2">
            {reasonOf(failure) === REASON_ADMIN_CANNOT_DISABLE_SELF
              ? "You cannot disable your own account."
              : "That change did not save. Try again."}
          </td>
        </tr>
      ) : null}
    </>
  );
}

// UsersTable is the admin's list of accounts, oldest first. Admins see accounts
// only, never another user's jobs, contacts, mail or drafts.
export function UsersTable() {
  const [tokens, setTokens] = useState<string[]>([""]);
  const pageToken = tokens[tokens.length - 1];
  const session = useQuery(AuthService.method.getSession);
  const users = useQuery(AdminService.method.listUsers, { pageSize: PAGE_SIZE, pageToken });
  const self = session.data?.session;

  if (session.isPending) {
    return <div aria-busy="true" className="p-[22px]" />;
  }
  if (self?.role !== "admin") {
    return (
      <p role="alert" className="p-[22px]">
        Only admins can manage users.
      </p>
    );
  }

  return (
    <section aria-labelledby="users-heading">
      <h2 id="users-heading" className="label border-b border-ink bg-strip px-[22px] py-3 font-medium">
        Users
      </h2>
      {users.isPending ? (
        <div aria-busy="true" className="p-[22px]">
          Loading users.
        </div>
      ) : users.isError ? (
        <div className="flex items-center gap-3 p-[22px]">
          <p role="alert">Could not load users.</p>
          <Button onClick={() => users.refetch()}>Retry</Button>
        </div>
      ) : (
        <>
          <div className="overflow-x-auto">
            <table aria-label="Users" className="w-full min-w-[860px] border-collapse text-left">
              <thead>
                <tr className="label h-9 bg-ink text-paper">
                  <th className="px-2.5 font-medium">Email</th>
                  <th className="px-2.5 font-medium">Name</th>
                  <th className="px-2.5 font-medium">Role</th>
                  <th className="px-2.5 font-medium">Status</th>
                  <th className="px-2.5 font-medium">Gmail</th>
                  <th className="px-2.5 font-medium">Joined</th>
                  <th className="px-2.5 text-right font-medium">
                    <span className="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {users.data.users.map((user) => (
                  <UserRow key={user.id} user={user} isSelf={user.email === self.email} />
                ))}
              </tbody>
            </table>
          </div>
          <div className="flex items-center gap-2 p-[22px]">
            <Button disabled={tokens.length === 1} onClick={() => setTokens(tokens.slice(0, -1))}>
              Previous
            </Button>
            <Button
              disabled={users.data.nextPageToken === ""}
              onClick={() => setTokens([...tokens, users.data.nextPageToken])}
            >
              Next
            </Button>
          </div>
        </>
      )}
    </section>
  );
}
