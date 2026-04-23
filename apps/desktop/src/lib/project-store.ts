import { create } from "zustand";
import { desktopApi } from "./desktop-client";

interface ProjectItem {
  id: string;
  title: string;
  description?: string;
  status: string;
  schedule_type: string;
  channel_id?: string;
  creator_owner_id: string;
  created_at: string;
  updated_at: string;
  plan?: { id: string; title: string; approval_status: string };
  active_run?: { id: string; status: string; start_at?: string };
}

interface ProjectVersion {
  id: string;
  project_id: string;
  version_number: number;
  branch_name?: string;
  version_status: string;
  created_at: string;
}

interface ProjectRun {
  id: string;
  project_id: string;
  status: string;
  start_at?: string;
  end_at?: string;
  failure_reason?: string;
  retry_count: number;
  created_at: string;
}

interface ProjectState {
  projects: ProjectItem[];
  currentProject: ProjectItem | null;
  versions: ProjectVersion[];
  runs: ProjectRun[];
  loading: boolean;
}

interface ProjectActions {
  fetchProjects: () => Promise<void>;
  fetchProject: (id: string) => Promise<void>;
  createProject: (data: {
    title: string;
    description?: string;
    schedule_type?: string;
  }) => Promise<ProjectItem>;
  updateProject: (id: string, data: Record<string, unknown>) => Promise<void>;
  deleteProject: (id: string) => Promise<void>;
  fetchVersions: (projectId: string) => Promise<void>;
  fetchRuns: (projectId: string) => Promise<void>;
  approvePlan: (projectId: string) => Promise<void>;
  rejectPlan: (projectId: string, reason: string) => Promise<void>;
}

export const useDesktopProjectStore = create<ProjectState & ProjectActions>((set, get) => ({
  projects: [],
  currentProject: null,
  versions: [],
  runs: [],
  loading: false,

  fetchProjects: async () => {
    set({ loading: true });
    try {
      const projects = (await desktopApi.listProjects()) as ProjectItem[];
      set({ projects });
    } finally {
      set({ loading: false });
    }
  },

  fetchProject: async (id: string) => {
    const project = (await desktopApi.getProject(id)) as ProjectItem;
    set({ currentProject: project });
  },

  createProject: async (data) => {
    const project = (await desktopApi.createProject(data)) as ProjectItem;
    set((s) => ({ projects: [project, ...s.projects] }));
    return project;
  },

  updateProject: async (id, data) => {
    const updated = (await desktopApi.updateProject(id, data)) as ProjectItem;
    set((s) => ({
      projects: s.projects.map((p) => (p.id === id ? updated : p)),
      currentProject: s.currentProject?.id === id ? updated : s.currentProject,
    }));
  },

  deleteProject: async (id) => {
    await desktopApi.deleteProject(id);
    set((s) => ({
      projects: s.projects.filter((p) => p.id !== id),
      currentProject: s.currentProject?.id === id ? null : s.currentProject,
    }));
  },

  fetchVersions: async (projectId) => {
    const versions = (await desktopApi.listProjectVersions(projectId)) as ProjectVersion[];
    set({ versions });
  },

  fetchRuns: async (projectId) => {
    const runs = (await desktopApi.listProjectRuns(projectId)) as ProjectRun[];
    set({ runs });
  },

  approvePlan: async (projectId) => {
    await desktopApi.approvePlan(projectId);
    void get().fetchProject(projectId);
  },

  rejectPlan: async (projectId, reason) => {
    await desktopApi.rejectPlan(projectId, reason);
    void get().fetchProject(projectId);
  },
}));
