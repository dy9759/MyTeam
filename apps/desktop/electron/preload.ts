import { contextBridge, ipcRenderer } from "electron";
import { buildDesktopApi } from "./preload-api";

contextBridge.exposeInMainWorld(
  "myteam",
  buildDesktopApi({
    invoke: (channel, ...args) => ipcRenderer.invoke(channel, ...args),
    send: (channel, ...args) => ipcRenderer.send(channel, ...args),
  }),
);
