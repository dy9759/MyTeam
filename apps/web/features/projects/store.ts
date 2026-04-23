"use client";

import { create } from "zustand";
import type { Project, ProjectVersion, ProjectRun, ProjectBranch, ProjectResult, ProjectContext, ProjectPR, ProjectShare, CreateProjectFromChatRequest } from "@/shared/types";
import { toast } from "sonner";
import { api } from "@/shared/api";
import { createLogger } from "@/shared/logger";

const logger = createLogger("project-store");

interface ProjectState {
  projects: Project[];
  currentProject: Project | null;
  versions: ProjectVersion[];
  runs: ProjectRun[];
  branches: ProjectBranch[];
  currentResult: ProjectResult | null;
  contexts: ProjectContext[];
  prs: ProjectPR[];
  shares: ProjectShare[];
  loading: boolean;
}

interface ProjectActions {
  fetch: () => Promise<void>;
  fetchProject: (id: string, signal?: AbortSignal) => Promise<void>;
  createProject: (data: { title: string; description?: string; schedule_type: string }) => Promise<Project>;
  createFromChat: (data: CreateProjectFromChatRequest) => Promise<Project>;
  updateProject: (id: string, data: Partial<Project>) => Promise<void>;
  deleteProject: (id: string) => Promise<void>;
  forkProject: (id: string, branchName: string, reason?: string) => Promise<void>;
  fetchVersions: (id: string, signal?: AbortSignal) => Promise<void>;
  fetchRuns: (id: string, signal?: AbortSignal) => Promise<void>;
  approvePlan: (projectId: string, planId: string) => Promise<void>;
  rejectPlan: (projectId: string, reason: string) => Promise<void>;
  fetchBranches: (projectId: string) => Promise<void>;
  fetchResult: (projectId: string, runId: string) => Promise<void>;
  fetchContexts: (projectId: string) => Promise<void>;
  importContext: (projectId: string, data: { source_type: string; source_id: string; date_from?: string; date_to?: string }) => Promise<void>;
  fetchPRs: (projectId: string) => Promise<void>;
  createPR: (projectId: string, data: { source_branch_id: string; target_branch_id: string; source_version_id: string; title: string; description?: string }) => Promise<ProjectPR>;
  mergePR: (projectId: string, prId: string) => Promise<void>;
  closePR: (projectId: string, prId: string) => Promise<void>;
  fetchShares: (projectId: string) => Promise<void>;
  shareProject: (projectId: string, data: { owner_id: string; role: string; can_merge_pr?: boolean }) => Promise<void>;
  removeShare: (projectId: string, ownerId: string) => Promise<void>;
}

export const useProjectStore = create<ProjectState & ProjectActions>((set, get) => ({
  projects: [],
  currentProject: null,
  versions: [],
  runs: [],
  branches: [],
  currentResult: null,
  contexts: [],
  prs: [],
  shares: [],
  loading: true,

  fetch: async () => {
    logger.debug("fetch projects start");
    const isInitialLoad = get().projects.length === 0;
    if (isInitialLoad) set({ loading: true });
    try {
      const data = await api.listProjects();
      const projects = Array.isArray(data) ? data : [];
      logger.info("fetched", projects.length, "projects");
      set({ projects, loading: false });
    } catch (err) {
      logger.error("fetch projects failed", err);
      toast.error("加载项目失败");
      if (isInitialLoad) set({ loading: false });
    }
  },

  fetchProject: async (id: string, signal?: AbortSignal) => {
    logger.debug("fetch project", id);
    try {
      const project = await api.getProject(id, signal ? { signal } : undefined);
      if (signal?.aborted) return;
      set({ currentProject: project });
    } catch (err) {
      if ((err as Error)?.name === "AbortError") return;
      logger.error("fetch project failed", err);
      toast.error("加载项目详情失败");
    }
  },

  createProject: async (data) => {
    const project = await api.createProject(data);
    set((s) => ({ projects: [...s.projects, project] }));
    return project;
  },

  createFromChat: async (data) => {
    const response = await api.createProjectFromChat(data);
    const project = response.project;
    set((s) => ({ projects: [...s.projects, project] }));
    return project;
  },

  updateProject: async (id, data) => {
    try {
      const updated = await api.updateProject(id, data);
      set((s) => ({
        projects: s.projects.map((p) => (p.id === id ? updated : p)),
        currentProject: s.currentProject?.id === id ? updated : s.currentProject,
      }));
    } catch (err) {
      logger.error("update project failed", err);
      toast.error("更新项目失败");
      throw err;
    }
  },

  deleteProject: async (id) => {
    try {
      await api.deleteProject(id);
      set((s) => ({
        projects: s.projects.filter((p) => p.id !== id),
        currentProject: s.currentProject?.id === id ? null : s.currentProject,
      }));
    } catch (err) {
      logger.error("delete project failed", err);
      toast.error("删除项目失败");
      throw err;
    }
  },

  forkProject: async (id, branchName, reason) => {
    try {
      const version = await api.forkProject(id, { branch_name: branchName, fork_reason: reason });
      set((s) => {
        // Guard: older api.listProjectVersions returned the raw
        // {versions,total} wrapper and poisoned state as an object.
        // Keep a tight invariant here so a stale session can't crash
        // the fork flow even before the user reloads.
        const prior = Array.isArray(s.versions) ? s.versions : [];
        return { versions: [...prior, version] };
      });
      toast.success("项目已分叉");
    } catch (err) {
      logger.error("fork project failed", err);
      toast.error("分叉项目失败");
      throw err;
    }
  },

  fetchVersions: async (id, signal) => {
    try {
      const versions = await api.listProjectVersions(id, signal ? { signal } : undefined);
      if (signal?.aborted) return;
      set({ versions });
    } catch (err) {
      if ((err as Error)?.name === "AbortError") return;
      logger.error("fetch versions failed", err);
    }
  },

  fetchRuns: async (id, signal) => {
    try {
      const runs = await api.listProjectRuns(id, signal ? { signal } : undefined);
      if (signal?.aborted) return;
      set({ runs });
    } catch (err) {
      if ((err as Error)?.name === "AbortError") return;
      logger.error("fetch runs failed", err);
    }
  },

  approvePlan: async (projectId, planId) => {
    try {
      await api.approvePlan(planId);
      // Re-fetch project to get updated plan status
      const project = await api.getProject(projectId);
      set({ currentProject: project });
      toast.success("计划已审批通过");
    } catch (err) {
      logger.error("approve plan failed", err);
      toast.error("审批计划失败");
      throw err;
    }
  },

  rejectPlan: async (projectId, reason) => {
    try {
      await api.rejectPlan(projectId, reason);
      const project = await api.getProject(projectId);
      set({ currentProject: project });
      toast.success("计划已拒绝");
    } catch (err) {
      logger.error("reject plan failed", err);
      toast.error("拒绝计划失败");
      throw err;
    }
  },

  fetchBranches: async (projectId: string) => {
    const branches = await api.listProjectBranches(projectId);
    set({ branches });
  },

  fetchResult: async (projectId: string, runId: string) => {
    try {
      const result = await api.getProjectResult(projectId, runId);
      set({ currentResult: result });
    } catch {
      set({ currentResult: null });
    }
  },

  fetchContexts: async (projectId: string) => {
    try {
      const contexts = await api.listProjectContexts(projectId);
      set({ contexts });
    } catch (err) {
      logger.error("fetch contexts failed", err);
    }
  },

  importContext: async (projectId: string, data) => {
    const context = await api.importProjectContext(projectId, data);
    set((s) => ({ contexts: [context, ...s.contexts] }));
  },

  fetchPRs: async (projectId: string) => {
    try {
      const prs = await api.listProjectPRs(projectId);
      set({ prs });
    } catch (err) {
      logger.error("fetch PRs failed", err);
    }
  },

  createPR: async (projectId: string, data) => {
    const pr = await api.createProjectPR(projectId, data);
    set((s) => ({ prs: [pr, ...s.prs] }));
    return pr;
  },

  mergePR: async (projectId: string, prId: string) => {
    await api.mergeProjectPR(projectId, prId);
    set((s) => ({
      prs: s.prs.map((pr) => pr.id === prId ? { ...pr, status: 'merged' as const } : pr),
    }));
  },

  closePR: async (projectId: string, prId: string) => {
    await api.closeProjectPR(projectId, prId);
    set((s) => ({
      prs: s.prs.map((pr) => pr.id === prId ? { ...pr, status: 'closed' as const } : pr),
    }));
  },

  fetchShares: async (projectId: string) => {
    const shares = await api.listProjectShares(projectId);
    set({ shares });
  },

  shareProject: async (projectId: string, data: { owner_id: string; role: string; can_merge_pr?: boolean }) => {
    const share = await api.shareProject(projectId, data);
    set((s) => ({ shares: [...s.shares, share] }));
  },

  removeShare: async (projectId: string, ownerId: string) => {
    await api.removeProjectShare(projectId, ownerId);
    set((s) => ({ shares: s.shares.filter((sh) => sh.owner_id !== ownerId) }));
  },
}));
