import { createJSONStorage, type StateStorage } from "zustand/middleware";

export const STORAGE_KEYS = {
  authToken: {
    current: "myteam_token",
    legacy: undefined,
  },
  workspaceId: {
    current: "myteam_workspace_id",
    legacy: undefined,
  },
  impersonateAgent: {
    current: "myteam_impersonate_agent",
    legacy: undefined,
  },
  loggedInCookie: {
    current: "myteam_logged_in",
    legacy: undefined,
  },
} as const;

const LEGACY_LOCAL_STORAGE_KEYS: Record<string, string> = {};

function getLocalStorage(): Storage | null {
  if (typeof window === "undefined") {
    return null;
  }
  return window.localStorage;
}

export function getLocalStorageValue(currentKey: string, legacyKey?: string): string | null {
  const storage = getLocalStorage();
  if (!storage) {
    return null;
  }

  const currentValue = storage.getItem(currentKey);
  if (currentValue !== null) {
    return currentValue;
  }

  if (!legacyKey) {
    return null;
  }

  const legacyValue = storage.getItem(legacyKey);
  if (legacyValue !== null) {
    storage.setItem(currentKey, legacyValue);
    storage.removeItem(legacyKey);
  }
  return legacyValue;
}

export function setLocalStorageValue(currentKey: string, value: string, legacyKey?: string) {
  const storage = getLocalStorage();
  if (!storage) {
    return;
  }
  storage.setItem(currentKey, value);
  if (legacyKey) {
    storage.removeItem(legacyKey);
  }
}

export function removeLocalStorageValue(currentKey: string, legacyKey?: string) {
  const storage = getLocalStorage();
  if (!storage) {
    return;
  }
  storage.removeItem(currentKey);
  if (legacyKey) {
    storage.removeItem(legacyKey);
  }
}

export const legacyAwareJSONStorage = createJSONStorage((): StateStorage => ({
  getItem(name) {
    const storage = getLocalStorage();
    if (!storage) {
      return null;
    }

    const currentValue = storage.getItem(name);
    if (currentValue !== null) {
      return currentValue;
    }

    const legacyKey = LEGACY_LOCAL_STORAGE_KEYS[name];
    if (!legacyKey) {
      return null;
    }

    const legacyValue = storage.getItem(legacyKey);
    if (legacyValue !== null) {
      storage.setItem(name, legacyValue);
      storage.removeItem(legacyKey);
    }
    return legacyValue;
  },
  setItem(name, value) {
    const storage = getLocalStorage();
    if (!storage) {
      return;
    }
    storage.setItem(name, value);
    const legacyKey = LEGACY_LOCAL_STORAGE_KEYS[name];
    if (legacyKey) {
      storage.removeItem(legacyKey);
    }
  },
  removeItem(name) {
    const storage = getLocalStorage();
    if (!storage) {
      return;
    }
    storage.removeItem(name);
    const legacyKey = LEGACY_LOCAL_STORAGE_KEYS[name];
    if (legacyKey) {
      storage.removeItem(legacyKey);
    }
  },
}));
