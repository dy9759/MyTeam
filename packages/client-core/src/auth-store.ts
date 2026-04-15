import { create } from "zustand";
import type { AuthApiClient, NativeSecrets, SessionUser } from "./host";

export interface AuthStoreState {
  user: SessionUser | null;
  token: string | null;
  isLoading: boolean;
  initialize: () => Promise<void>;
  setSession: (token: string, user: SessionUser) => Promise<void>;
  logout: () => Promise<void>;
  setUser: (user: SessionUser | null) => void;
}

export function createAuthStore({
  api,
  secrets,
}: {
  api: AuthApiClient;
  secrets: NativeSecrets;
}) {
  return create<AuthStoreState>((set) => ({
    user: null,
    token: null,
    isLoading: true,

    initialize: async () => {
      const token = await secrets.getToken();
      if (!token) {
        api.setToken(null);
        set({ user: null, token: null, isLoading: false });
        return;
      }

      api.setToken(token);
      try {
        const user = await api.getMe();
        set({ user, token, isLoading: false });
      } catch {
        await secrets.deleteToken();
        api.setToken(null);
        api.setWorkspaceId(null);
        set({ user: null, token: null, isLoading: false });
      }
    },

    setSession: async (token, user) => {
      await secrets.setToken(token);
      api.setToken(token);
      set({ user, token, isLoading: false });
    },

    logout: async () => {
      await secrets.deleteToken();
      api.setToken(null);
      api.setWorkspaceId(null);
      set({ user: null, token: null, isLoading: false });
    },

    setUser: (user) => {
      set({ user });
    },
  }));
}
