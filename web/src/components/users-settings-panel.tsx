import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";

import { apiClient, type LocalUser, type MeResponse } from "../lib/api";
import { ErrorText, Field, ModalFrame, SelectField, SettingsState } from "./settings-shared";

type CreateUserDraft = {
  username: string;
  role: string;
  password: string;
};

type EditUserDraft = {
  username: string;
  role: string;
  new_password: string;
};

const emptyCreateDraft: CreateUserDraft = {
  username: "",
  role: "operator",
  password: ""
};

const emptyEditDraft: EditUserDraft = {
  username: "",
  role: "operator",
  new_password: ""
};

export function UsersSettingsPanel(props: { me: MeResponse }) {
  const queryClient = useQueryClient();
  const [createOpen, setCreateOpen] = useState(false);
  const [editUser, setEditUser] = useState<LocalUser | null>(null);
  const [createDraft, setCreateDraft] = useState<CreateUserDraft>(emptyCreateDraft);
  const [editDraft, setEditDraft] = useState<EditUserDraft>(emptyEditDraft);

  const usersQuery = useQuery({
    queryKey: ["users"],
    queryFn: () => apiClient.users()
  });

  useEffect(() => {
    if (!editUser) {
      return;
    }

    setEditDraft({
      username: editUser.username,
      role: editUser.role,
      new_password: ""
    });
  }, [editUser]);

  const createUserMutation = useMutation({
    mutationFn: () => apiClient.createUser(createDraft),
    onSuccess: async () => {
      setCreateOpen(false);
      setCreateDraft(emptyCreateDraft);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["users"] }),
        queryClient.invalidateQueries({ queryKey: ["audit"] })
      ]);
    }
  });

  const updateUserMutation = useMutation({
    mutationFn: () => {
      if (!editUser) {
        throw new Error("No user selected");
      }

      return apiClient.updateUser(editUser.id, {
        username: editDraft.username,
        role: editDraft.role,
        new_password: editDraft.new_password || undefined
      });
    },
    onSuccess: async () => {
      setEditUser(null);
      setEditDraft(emptyEditDraft);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["users"] }),
        queryClient.invalidateQueries({ queryKey: ["audit"] })
      ]);
    }
  });

  const deleteUserMutation = useMutation({
    mutationFn: (userID: string) => apiClient.deleteUser(userID),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["users"] }),
        queryClient.invalidateQueries({ queryKey: ["audit"] })
      ]);
    }
  });

  const resetUserTotpMutation = useMutation({
    mutationFn: (userID: string) => apiClient.resetUserTotp(userID),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["users"] }),
        queryClient.invalidateQueries({ queryKey: ["audit"] })
      ]);
    }
  });

  if (usersQuery.isLoading) {
    return <SettingsState title="Loading local users" description="Refreshing local accounts and their access state." />;
  }

  if (usersQuery.isError || !usersQuery.data) {
    return <SettingsState title="Users are unavailable" description="The control-plane could not load local accounts right now." />;
  }

  const mutationError =
    createUserMutation.error?.message ??
    updateUserMutation.error?.message ??
    deleteUserMutation.error?.message ??
    resetUserTotpMutation.error?.message ??
    null;

  return (
    <>
      <div className="rounded-3xl border border-slate-200 bg-slate-50/80 px-5 py-5">
        <div className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
          <div>
            <div className="text-base font-semibold text-slate-950">Local users</div>
            <p className="mt-1 text-sm leading-6 text-slate-600">
              Keep local access compact and manageable with a short list of accounts and small edit flows.
            </p>
          </div>
          <span className="rounded-full border border-slate-200 bg-white px-3 py-1 text-xs font-semibold uppercase tracking-[0.2em] text-slate-500">
            {usersQuery.data.length} {usersQuery.data.length === 1 ? "user" : "users"}
          </span>
        </div>

        <div className="mt-5 border-t border-slate-200 pt-5">
        <div className="flex items-center justify-between gap-4">
          <p className="text-sm text-slate-600">
            Most panels only need a couple of local accounts, so create and edit flows stay close by in small modals.
          </p>
          <button
            type="button"
            className="rounded-2xl bg-slate-950 px-5 py-3 text-sm font-medium text-white transition hover:bg-slate-800"
            onClick={() => setCreateOpen(true)}
          >
            Add user
          </button>
        </div>

        <div className="mt-6 overflow-x-auto">
          <table className="min-w-full border-separate border-spacing-y-3">
            <thead>
              <tr className="text-left text-xs font-semibold uppercase tracking-[0.22em] text-slate-500">
                <th className="px-4 pb-1">User</th>
                <th className="px-4 pb-1">Role</th>
                <th className="px-4 pb-1">TOTP</th>
                <th className="px-4 pb-1">Actions</th>
              </tr>
            </thead>
            <tbody>
              {usersQuery.data.map((user) => {
                const isCurrentUser = user.id === props.me.id;
                return (
                  <tr key={user.id} className="rounded-3xl bg-slate-50 text-sm text-slate-700">
                    <td className="rounded-l-3xl px-4 py-4">
                      <div className="font-medium text-slate-950">{user.username}</div>
                      <div className="mt-1 text-xs uppercase tracking-[0.2em] text-slate-500">{isCurrentUser ? "Current account" : user.id}</div>
                    </td>
                    <td className="px-4 py-4 capitalize">{user.role}</td>
                    <td className="px-4 py-4">{user.totp_enabled ? "Enabled" : "Disabled"}</td>
                    <td className="rounded-r-3xl px-4 py-4">
                      <div className="flex flex-wrap gap-2">
                        <button
                          type="button"
                          className="rounded-2xl border border-slate-300 px-4 py-2 text-sm font-medium text-slate-800 transition hover:border-slate-400 hover:bg-white"
                          onClick={() => setEditUser(user)}
                        >
                          Edit
                        </button>
                        <button
                          type="button"
                          className="rounded-2xl border border-slate-300 px-4 py-2 text-sm font-medium text-slate-800 transition hover:border-slate-400 hover:bg-white disabled:cursor-not-allowed disabled:opacity-50"
                          onClick={() => resetUserTotpMutation.mutate(user.id)}
                          disabled={isCurrentUser || resetUserTotpMutation.isPending}
                          title={isCurrentUser ? "Use the Security tab to manage TOTP for your own account." : undefined}
                        >
                          {resetUserTotpMutation.isPending && resetUserTotpMutation.variables === user.id ? "Resetting..." : "Reset TOTP"}
                        </button>
                        <button
                          type="button"
                          className="rounded-2xl border border-rose-200 px-4 py-2 text-sm font-medium text-rose-700 transition hover:bg-rose-50 disabled:cursor-not-allowed disabled:opacity-50"
                          onClick={() => {
                            if (window.confirm(`Delete local user "${user.username}"?`)) {
                              deleteUserMutation.mutate(user.id);
                            }
                          }}
                          disabled={isCurrentUser || deleteUserMutation.isPending}
                          title={isCurrentUser ? "You cannot delete the account that is currently signed in." : undefined}
                        >
                          {deleteUserMutation.isPending && deleteUserMutation.variables === user.id ? "Deleting..." : "Delete"}
                        </button>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>

        {mutationError ? <div className="mt-4"><ErrorText message={mutationError} /></div> : null}
        </div>
      </div>

      <ModalFrame
        open={createOpen}
        onOpenChange={(open) => {
          setCreateOpen(open);
          if (!open) {
            setCreateDraft(emptyCreateDraft);
            createUserMutation.reset();
          }
        }}
        title="Add local user"
        description="Create a compact local account for someone who needs direct access to the panel."
      >
        <div className="space-y-4">
          <Field label="Username" value={createDraft.username} onChange={(value) => setCreateDraft((draft) => ({ ...draft, username: value }))} />
          <SelectField
            label="Role"
            value={createDraft.role}
            onChange={(value) => setCreateDraft((draft) => ({ ...draft, role: value }))}
            options={[
              { value: "viewer", label: "Viewer" },
              { value: "operator", label: "Operator" },
              { value: "admin", label: "Admin" }
            ]}
          />
          <Field
            label="Password"
            type="password"
            value={createDraft.password}
            onChange={(value) => setCreateDraft((draft) => ({ ...draft, password: value }))}
          />
          {createUserMutation.error ? <ErrorText message={createUserMutation.error.message} /> : null}
          <div className="flex justify-end">
            <button
              type="button"
              className="rounded-2xl bg-slate-950 px-5 py-3 text-sm font-medium text-white transition hover:bg-slate-800 disabled:opacity-60"
              onClick={() => createUserMutation.mutate()}
              disabled={createUserMutation.isPending}
            >
              {createUserMutation.isPending ? "Creating..." : "Create user"}
            </button>
          </div>
        </div>
      </ModalFrame>

      <ModalFrame
        open={Boolean(editUser)}
        onOpenChange={(open) => {
          if (!open) {
            setEditUser(null);
            setEditDraft(emptyEditDraft);
            updateUserMutation.reset();
          }
        }}
        title={editUser ? `Edit ${editUser.username}` : "Edit user"}
        description="Update the username, role, or password for this local account."
      >
        <div className="space-y-4">
          <Field label="Username" value={editDraft.username} onChange={(value) => setEditDraft((draft) => ({ ...draft, username: value }))} />
          <SelectField
            label="Role"
            value={editDraft.role}
            onChange={(value) => setEditDraft((draft) => ({ ...draft, role: value }))}
            options={[
              { value: "viewer", label: "Viewer" },
              { value: "operator", label: "Operator" },
              { value: "admin", label: "Admin" }
            ]}
          />
          <Field
            label="New password"
            type="password"
            value={editDraft.new_password}
            onChange={(value) => setEditDraft((draft) => ({ ...draft, new_password: value }))}
            helperText="Leave empty to keep the current password."
          />
          {updateUserMutation.error ? <ErrorText message={updateUserMutation.error.message} /> : null}
          <div className="flex justify-end">
            <button
              type="button"
              className="rounded-2xl bg-slate-950 px-5 py-3 text-sm font-medium text-white transition hover:bg-slate-800 disabled:opacity-60"
              onClick={() => updateUserMutation.mutate()}
              disabled={updateUserMutation.isPending || !editUser}
            >
              {updateUserMutation.isPending ? "Saving..." : "Save user"}
            </button>
          </div>
        </div>
      </ModalFrame>
    </>
  );
}
