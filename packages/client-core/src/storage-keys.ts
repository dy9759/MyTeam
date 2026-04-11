import type { SessionStorageLike } from "./host";

export const WORKSPACE_STORAGE_KEY = "myteam_workspace_id";

export function readStoredWorkspaceId(storage: SessionStorageLike): string | null {
  return storage.getItem(WORKSPACE_STORAGE_KEY);
}

export function writeStoredWorkspaceId(storage: SessionStorageLike, workspaceId: string) {
  storage.setItem(WORKSPACE_STORAGE_KEY, workspaceId);
}

export function clearStoredWorkspaceId(storage: SessionStorageLike) {
  storage.removeItem(WORKSPACE_STORAGE_KEY);
}
