"use client";

import { create } from "zustand";
import type { FileIndex } from "@/shared/types";
import { api } from "@/shared/api";
import { toast } from "sonner";

type FileTab = "my" | "agent" | "project";

interface FileState {
  files: FileIndex[];
  myFiles: FileIndex[];
  projectFiles: FileIndex[];
  loading: boolean;
  activeTab: FileTab;
  fetchFiles: (params?: { source_type?: string; project_id?: string; channel_id?: string }) => Promise<void>;
  fetchMyFiles: () => Promise<void>;
  fetchProjectFiles: (projectId: string) => Promise<void>;
  setActiveTab: (tab: FileTab) => void;
}

export const useFileStore = create<FileState>((set, get) => ({
  files: [],
  myFiles: [],
  projectFiles: [],
  loading: false,
  activeTab: "my",

  fetchFiles: async (params) => {
    set({ loading: true });
    try {
      const files = await api.listFiles(params);
      set({ files, loading: false });
    } catch {
      toast.error("Failed to load files");
      set({ loading: false });
    }
  },

  fetchMyFiles: async () => {
    set({ loading: true });
    try {
      const myFiles = await api.listMyFiles();
      set({ myFiles, loading: false });
    } catch {
      toast.error("Failed to load my files");
      set({ loading: false });
    }
  },

  fetchProjectFiles: async (projectId: string) => {
    set({ loading: true });
    try {
      const projectFiles = await api.listProjectFiles(projectId);
      set({ projectFiles, loading: false });
    } catch {
      toast.error("Failed to load project files");
      set({ loading: false });
    }
  },

  setActiveTab: (tab) => set({ activeTab: tab }),
}));
