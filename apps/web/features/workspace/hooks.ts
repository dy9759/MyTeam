"use client";

import { useCallback } from "react";
import { useAuthStore } from "@/features/auth";
import { api } from "@/shared/api";
import type { CreateMemberRequest, MemberRole, WorkspaceRepo } from "@/shared/types";
import { useWorkspaceStore } from "./store";

type WorkspaceUpdateInput = {
  name?: string;
  description?: string;
  context?: string;
  settings?: Record<string, unknown>;
  repos?: WorkspaceRepo[];
};

type WorkspaceSettingsUpdater =
  | Record<string, unknown>
  | ((current: Record<string, unknown>) => Record<string, unknown>);

export function useWorkspaceManagement() {
  const user = useAuthStore((s) => s.user);
  const workspace = useWorkspaceStore((s) => s.workspace);
  const members = useWorkspaceStore((s) => s.members);
  const syncWorkspace = useWorkspaceStore((s) => s.updateWorkspace);
  const refreshMembers = useWorkspaceStore((s) => s.refreshMembers);

  const currentMember = members.find((member) => member.user_id === user?.id) ?? null;
  const canManageWorkspace = currentMember?.role === "owner" || currentMember?.role === "admin";
  const isOwner = currentMember?.role === "owner";

  const requireWorkspace = useCallback(() => {
    if (!workspace) {
      throw new Error("No workspace selected");
    }
    return workspace;
  }, [workspace]);

  const saveWorkspace = useCallback(
    async (data: WorkspaceUpdateInput) => {
      const currentWorkspace = requireWorkspace();
      const updated = await api.updateWorkspace(currentWorkspace.id, data);
      syncWorkspace(updated);
      return updated;
    },
    [requireWorkspace, syncWorkspace],
  );

  const saveWorkspaceSettings = useCallback(
    async (nextSettings: WorkspaceSettingsUpdater) => {
      const currentWorkspace = requireWorkspace();
      const currentSettings = (currentWorkspace.settings ?? {}) as Record<string, unknown>;
      const resolvedSettings =
        typeof nextSettings === "function" ? nextSettings(currentSettings) : nextSettings;
      return saveWorkspace({ settings: resolvedSettings });
    },
    [requireWorkspace, saveWorkspace],
  );

  const inviteMember = useCallback(
    async (data: CreateMemberRequest) => {
      const currentWorkspace = requireWorkspace();
      const createdMember = await api.createMember(currentWorkspace.id, data);
      await refreshMembers();
      return createdMember;
    },
    [refreshMembers, requireWorkspace],
  );

  const changeMemberRole = useCallback(
    async (memberId: string, role: MemberRole) => {
      const currentWorkspace = requireWorkspace();
      const updatedMember = await api.updateMember(currentWorkspace.id, memberId, { role });
      await refreshMembers();
      return updatedMember;
    },
    [refreshMembers, requireWorkspace],
  );

  const removeMember = useCallback(
    async (memberId: string) => {
      const currentWorkspace = requireWorkspace();
      await api.deleteMember(currentWorkspace.id, memberId);
      await refreshMembers();
    },
    [refreshMembers, requireWorkspace],
  );

  return {
    workspace,
    members,
    currentMember,
    canManageWorkspace,
    isOwner,
    saveWorkspace,
    saveWorkspaceSettings,
    inviteMember,
    changeMemberRole,
    removeMember,
  };
}

export function useActorName() {
  const members = useWorkspaceStore((s) => s.members);
  const agents = useWorkspaceStore((s) => s.agents);

  const getMemberName = (userId: string) => {
    const m = members.find((m) => m.user_id === userId);
    return m?.name ?? "Unknown";
  };

  const getAgentName = (agentId: string) => {
    const a = agents.find((a) => a.id === agentId);
    return a?.name ?? "Unknown Agent";
  };

  const getActorName = (type: string, id: string) => {
    if (type === "member") return getMemberName(id);
    if (type === "agent") return getAgentName(id);
    return "System";
  };

  const getActorInitials = (type: string, id: string) => {
    const name = getActorName(type, id);
    return name
      .split(" ")
      .map((w) => w[0])
      .join("")
      .toUpperCase()
      .slice(0, 2);
  };

  const getActorAvatarUrl = (type: string, id: string): string | null => {
    if (type === "member") return members.find((m) => m.user_id === id)?.avatar_url ?? null;
    if (type === "agent") return agents.find((a) => a.id === id)?.avatar_url ?? null;
    return null;
  };

  return { getMemberName, getAgentName, getActorName, getActorInitials, getActorAvatarUrl };
}
