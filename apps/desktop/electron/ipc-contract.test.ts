import { describe, expect, it, vi } from "vitest";
import { buildDesktopApi, IPC_CHANNELS } from "./preload-api";

describe("buildDesktopApi", () => {
  it("routes auth and runtime calls through the expected IPC channels", async () => {
    const invoke = vi.fn().mockResolvedValue(undefined);
    const send = vi.fn();
    const api = buildDesktopApi({ invoke, send });

    await api.auth.sendCode("dev@myteam.ai");
    await api.auth.verifyCode("dev@myteam.ai", "123456");
    await api.auth.setStoredToken("myt_test");
    await api.runtime.startDaemon();
    await api.files.revealPath("/tmp/demo.txt");
    api.notifications.show({
      title: "My Team",
      body: "Desktop notification",
    });

    expect(invoke).toHaveBeenCalledWith(
      IPC_CHANNELS.authSendCode,
      "dev@myteam.ai",
    );
    expect(invoke).toHaveBeenCalledWith(
      IPC_CHANNELS.authVerifyCode,
      "dev@myteam.ai",
      "123456",
    );
    expect(invoke).toHaveBeenCalledWith(
      IPC_CHANNELS.authSetStoredToken,
      "myt_test",
    );
    expect(invoke).toHaveBeenCalledWith(IPC_CHANNELS.runtimeStartDaemon);
    expect(invoke).toHaveBeenCalledWith(
      IPC_CHANNELS.fileRevealPath,
      "/tmp/demo.txt",
    );
    expect(send).toHaveBeenCalledWith(IPC_CHANNELS.notificationShow, {
      title: "My Team",
      body: "Desktop notification",
    });
  });
});
