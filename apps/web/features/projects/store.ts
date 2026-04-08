"use client";

import { create } from "zustand";
import type { Project, ProjectVersion, ProjectRun, CreateProjectFromChatRequest } from "@/shared/types";
import { toast } from "sonner";
import { api } from "@/shared/api";
import { createLogger } from "@/shared/logger";

const logger = createLogger("project-store");

interface ProjectState {
  projects: Project[];
  currentProject: Project | null;
  versions: ProjectVersion[];
  runs: ProjectRun[];
  loading: boolean;
}

interface ProjectActions {
  fetch: () => Promise<void>;
  fetchProject: (id: string) => Promise<void>;
  createProject: (data: { title: string; description?: string; schedule_type: string }) => Promise<Project>;
  createFromChat: (data: CreateProjectFromChatRequest) => Promise<Project>;
  updateProject: (id: string, data: Partial<Project>) => Promise<void>;
  deleteProject: (id: string) => Promise<void>;
  forkProject: (id: string, branchName: string, reason?: string) => Promise<void>;
  fetchVersions: (id: string) => Promise<void>;
  fetchRuns: (id: string) => Promise<void>;
  approvePlan: (projectId: string) => Promise<void>;
  rejectPlan: (projectId: string, reason: string) => Promise<void>;
}

export const useProjectStore = create<ProjectState & ProjectActions>((set, get) => ({
  projects: [],
  currentProject: null,
  versions: [],
  runs: [],
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

  fetchProject: async (id: string) => {
    logger.debug("fetch project", id);
    try {
      const project = await api.getProject(id);
      set({ currentProject: project });
    } catch (err) {
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
    const project = await api.createProjectFromChat(data);
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
    }
  },

  forkProject: async (id, branchName, reason) => {
    try {
      const version = await api.forkProject(id, { branch_name: branchName, fork_reason: reason });
      set((s) => ({ versions: [...s.versions, version] }));
      toast.success("项目已分叉");
    } catch (err) {
      logger.error("fork project failed", err);
      toast.error("分叉项目失败");
    }
  },

  fetchVersions: async (id) => {
    try {
      const versions = await api.listProjectVersions(id);
      set({ versions });
    } catch (err) {
      logger.error("fetch versions failed", err);
    }
  },

  fetchRuns: async (id) => {
    try {
      const runs = await api.listProjectRuns(id);
      set({ runs });
    } catch (err) {
      logger.error("fetch runs failed", err);
    }
  },

  approvePlan: async (projectId) => {
    try {
      await api.approvePlan(projectId);
      // Re-fetch project to get updated plan status
      const project = await api.getProject(projectId);
      set({ currentProject: project });
      toast.success("计划已审批通过");
    } catch (err) {
      logger.error("approve plan failed", err);
      toast.error("审批计划失败");
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
    }
  },
}));
