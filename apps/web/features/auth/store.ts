"use client";

import { create } from "zustand";
import type { User } from "@/shared/types";
import { api } from "@/shared/api";
import { createLogger } from "@/shared/logger";
import { setLoggedInCookie, clearLoggedInCookie } from "./auth-cookie";

const log = createLogger("auth:store");

interface AuthState {
  user: User | null;
  isLoading: boolean;

  initialize: () => Promise<void>;
  sendCode: (email: string) => Promise<void>;
  verifyCode: (email: string, code: string) => Promise<User>;
  logout: () => void;
  setUser: (user: User) => void;
}

export const useAuthStore = create<AuthState>((set) => ({
  user: null,
  isLoading: true,

  initialize: async () => {
    const token = localStorage.getItem("myteam_token");
    if (!token) {
      log.info("initialize: no token found, skipping");
      set({ isLoading: false });
      return;
    }

    log.info("initialize: token found, verifying via getMe()");
    api.setToken(token);

    try {
      const user = await api.getMe();
      log.info("initialize: session valid", { userId: user.id, email: user.email });
      set({ user, isLoading: false });
    } catch (err) {
      log.error("initialize: session invalid, clearing credentials", err);
      api.setToken(null);
      api.setWorkspaceId(null);
      localStorage.removeItem("myteam_token");
      localStorage.removeItem("myteam_workspace_id");
      set({ user: null, isLoading: false });
    }
  },

  sendCode: async (email: string) => {
    log.info("sendCode: requesting verification code", { email });
    try {
      await api.sendCode(email);
      log.info("sendCode: code sent successfully", { email });
    } catch (err) {
      log.error("sendCode: failed", { email, error: err instanceof Error ? err.message : err });
      throw err;
    }
  },

  verifyCode: async (email: string, code: string) => {
    log.info("verifyCode: verifying code", { email, codeLength: code.length });
    try {
      const { token, user } = await api.verifyCode(email, code);
      log.info("verifyCode: success", { userId: user.id, email: user.email });
      localStorage.setItem("myteam_token", token);
      api.setToken(token);
      setLoggedInCookie();
      set({ user });
      return user;
    } catch (err) {
      log.error("verifyCode: failed", { email, error: err instanceof Error ? err.message : err });
      throw err;
    }
  },

  logout: () => {
    log.info("logout: clearing session");
    localStorage.removeItem("myteam_token");
    localStorage.removeItem("myteam_workspace_id");
    api.setToken(null);
    api.setWorkspaceId(null);
    clearLoggedInCookie();
    set({ user: null });
  },

  setUser: (user: User) => {
    set({ user });
  },
}));
