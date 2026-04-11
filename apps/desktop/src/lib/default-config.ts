export type DesktopRuntimeMode = "development" | "production";

export type DesktopConfigEnv = Partial<Record<string, string | boolean | undefined>> & Partial<{
  MYTEAM_API_URL: string;
  MYTEAM_APP_URL: string;
  MYTEAM_WS_URL: string;
  VITE_MYTEAM_API_URL: string;
  VITE_MYTEAM_APP_URL: string;
  VITE_MYTEAM_WS_URL: string;
}>;

export interface DesktopConfig {
  apiBaseUrl: string;
  appUrl: string;
  wsUrl: string;
}

export function resolveDesktopConfig(
  mode: DesktopRuntimeMode,
  env: DesktopConfigEnv,
): DesktopConfig {
  const defaultApiBaseUrl =
    mode === "development" ? "http://localhost:8080" : "https://api.myteam.ai";
  const defaultAppUrl =
    mode === "development" ? "http://localhost:3000" : "https://myteam.ai";

  const apiBaseUrl = env.MYTEAM_API_URL ?? env.VITE_MYTEAM_API_URL ?? defaultApiBaseUrl;
  const appUrl = env.MYTEAM_APP_URL ?? env.VITE_MYTEAM_APP_URL ?? defaultAppUrl;
  const wsUrl =
    env.MYTEAM_WS_URL ??
    env.VITE_MYTEAM_WS_URL ??
    apiBaseUrl.replace(/^http/, "ws") + "/ws";

  return {
    apiBaseUrl,
    appUrl,
    wsUrl,
  };
}
