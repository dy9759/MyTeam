/// <reference types="vite/client" />

import type { DesktopApi } from "../electron/preload-api";

declare global {
  interface Window {
    myteam: DesktopApi;
  }
}

export {};
