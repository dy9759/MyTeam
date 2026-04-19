"use client";

import { create as createStore } from "zustand";
import {
  createMemory,
  listMemories,
  MemoryApiError,
  promoteMemory,
  searchMemories,
  type CreateMemoryInput,
  type Hit,
  type Memory,
  type MemoryFilter,
  type SearchInput,
} from "@/features/memories/api";

type MemoryState = {
  memories: Memory[];
  loading: boolean;
  error: string | null;
  searchResults: Hit[];
  searchLoading: boolean;
  searchError: string | null;
  fetchAll: (filter?: MemoryFilter) => Promise<void>;
  create: (input: CreateMemoryInput) => Promise<Memory | null>;
  promote: (id: string) => Promise<Memory | null>;
  search: (query: SearchInput) => Promise<Hit[]>;
};

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error ? error.message : fallback;
}

export const useMemoryStore = createStore<MemoryState>((set) => ({
  memories: [],
  loading: false,
  error: null,
  searchResults: [],
  searchLoading: false,
  searchError: null,

  fetchAll: async (filter) => {
    set({ loading: true, error: null });
    try {
      const memories = await listMemories(filter);
      set({ memories, loading: false });
    } catch (error) {
      set({
        error: errorMessage(error, "Failed to load memories"),
        loading: false,
      });
    }
  },

  create: async (input) => {
    try {
      const memory = await createMemory(input);
      set((state) => ({ memories: [memory, ...state.memories] }));
      return memory;
    } catch (error) {
      set({ error: errorMessage(error, "Failed to create memory") });
      return null;
    }
  },

  promote: async (id) => {
    try {
      const memory = await promoteMemory(id);
      set((state) => ({
        memories: state.memories.map((item) =>
          item.id === id ? memory : item,
        ),
      }));
      return memory;
    } catch (error) {
      set({ error: errorMessage(error, "Failed to promote memory") });
      return null;
    }
  },

  search: async (query) => {
    const trimmedQuery = query.query.trim();
    if (!trimmedQuery) {
      set({ searchResults: [], searchLoading: false, searchError: null });
      return [];
    }

    set({ searchLoading: true, searchError: null });
    try {
      const hits = await searchMemories({ ...query, query: trimmedQuery });
      set({ searchResults: hits, searchLoading: false });
      return hits;
    } catch (error) {
      const searchError =
        error instanceof MemoryApiError && error.status === 503
          ? "Search not available — indexing not configured"
          : errorMessage(error, "Failed to search memories");
      set({ searchResults: [], searchLoading: false, searchError });
      return [];
    }
  },
}));
