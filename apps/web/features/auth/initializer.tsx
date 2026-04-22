"use client";

import { useEffect, type ReactNode } from "react";
import { useAuthStore } from "./store";
import { useWorkspaceStore } from "@/features/workspace";
import { api } from "@/shared/api";
import { createLogger } from "@/shared/logger";
import { setLoggedInCookie, clearLoggedInCookie } from "./auth-cookie";

const logger = createLogger("auth");

/**
 * Initializes auth + workspace state from localStorage on mount.
 * Fires getMe() and listWorkspaces() in parallel when a cached token exists.
 */
export function AuthInitializer({ children }: { children: ReactNode }) {
  useEffect(() => {
    const token = localStorage.getItem("myteam_token");
    if (!token) {
      logger.info("no stored token, skipping auth init");
      clearLoggedInCookie();
      useAuthStore.setState({ isLoading: false });
      return;
    }

    logger.info("found stored token, restoring session...");
    api.setToken(token);
    const wsId = localStorage.getItem("myteam_workspace_id");
    logger.debug("stored workspace id", { wsId });

    // Fire getMe and listWorkspaces in parallel
    const mePromise = api.getMe();
    const wsPromise = api.listWorkspaces();

    Promise.all([mePromise, wsPromise])
      .then(([user, wsList]) => {
        logger.info("session restored", { userId: user.id, email: user.email, workspaces: wsList.length });
        setLoggedInCookie();
        useAuthStore.setState({ user, isLoading: false });
        useWorkspaceStore.getState().hydrateWorkspace(wsList, wsId);
      })
      .catch((err) => {
        logger.error("auth init failed — token expired or backend unreachable", err);
        api.setToken(null);
        api.setWorkspaceId(null);
        localStorage.removeItem("myteam_token");
        localStorage.removeItem("myteam_workspace_id");
        clearLoggedInCookie();
        useAuthStore.setState({ user: null, isLoading: false });
      });
  }, []);

  return <>{children}</>;
}
