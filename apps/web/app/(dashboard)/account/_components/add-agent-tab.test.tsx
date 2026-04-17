import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AddAgentTab } from "./add-agent-tab";
import type { AgentRuntime } from "@/shared/types";

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

vi.mock("@/shared/api", () => ({
  api: {
    createAgent: vi.fn(),
  },
}));

const runtimes: AgentRuntime[] = [
  {
    id: "runtime-1",
    workspace_id: "ws-1",
    daemon_id: "daemon-1",
    name: "Local Codex Runtime",
    mode: "local",
    provider: "codex",
    status: "online",
    device_info: "MacBook Pro",
    server_host: "mbp.local",
    working_dir: "/Users/tester/myteam",
    capabilities: ["code", "review"],
    readiness: "ready",
    metadata: {
      display_name: "Codex CLI",
      version: "1.2.3",
    },
    last_heartbeat_at: "2026-04-10T00:00:00Z",
    concurrency_limit: 1,
    current_load: 0,
    lease_expires_at: null,
    created_at: "2026-04-10T00:00:00Z",
    updated_at: "2026-04-10T00:00:00Z",
  },
];

describe("AddAgentTab", () => {
  it("explains manual CLI setup and agent-assisted setup paths", () => {
    render(
      <AddAgentTab
        runtimes={runtimes}
        canManageWorkspace
        presetRuntimeId="runtime-1"
        onCreated={vi.fn()}
      />,
    );

    expect(screen.getByText("绑定到 MyTeam 平台")).toBeInTheDocument();
    expect(screen.getByText("本机 / CLI -> Runtime -> Agent")).toBeInTheDocument();
    expect(screen.getByText(/Runtime 是执行宿主，Agent 是工作区里的协作身份/)).toBeInTheDocument();
    expect(screen.getByText("方式一：你在 CLI 手动运行")).toBeInTheDocument();
    expect(screen.getByText("方式二：让 Agent 自动配置")).toBeInTheDocument();
    expect(screen.getByText("如果提示 myteam: command not found")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "复制“如果提示 myteam: command not found”命令" })).toBeInTheDocument();
    expect(screen.getByText("把 myteam 放进 PATH")).toBeInTheDocument();
    expect(screen.getByText("默认云端登录并启动 Runtime")).toBeInTheDocument();
    expect(screen.getByText(/默认云端登录站点走 `https:\/\/myteam\.ai`/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "复制“默认云端登录并启动 Runtime”命令" })).toBeInTheDocument();
    expect(screen.getByText("本地开发 / 自部署时改地址")).toBeInTheDocument();
    expect(screen.getByText(/请在当前机器上帮我完成 MyTeam runtime 接入/)).toBeInTheDocument();
    expect(screen.getByText(/默认把登录站点设为 https:\/\/myteam\.ai/)).toBeInTheDocument();
    expect(
      screen.getByText(/myteam agent create --name "Code Review Agent" --runtime-id YOUR_RUNTIME_ID/),
    ).toBeInTheDocument();
  });

  it("copies snippet content with one click", async () => {
    const user = userEvent.setup();
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(window.navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });

    render(
      <AddAgentTab
        runtimes={runtimes}
        canManageWorkspace
        presetRuntimeId="runtime-1"
        onCreated={vi.fn()}
      />,
    );

    await user.click(screen.getByRole("button", { name: "复制“默认云端登录并启动 Runtime”命令" }));

    expect(writeText).toHaveBeenCalledWith(`myteam config set app_url https://myteam.ai
myteam config set server_url https://api.myteam.ai
myteam login
myteam daemon start
myteam runtime list`);
  });

  it("copies the bootstrap commands for missing myteam CLI", async () => {
    const user = userEvent.setup();
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(window.navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });

    render(
      <AddAgentTab
        runtimes={runtimes}
        canManageWorkspace
        presetRuntimeId="runtime-1"
        onCreated={vi.fn()}
      />,
    );

    await user.click(screen.getByRole("button", { name: "复制“如果提示 myteam: command not found”命令" }));

    expect(writeText).toHaveBeenCalledWith(`cd /path/to/MyTeam
make build
./server/bin/myteam config set app_url https://myteam.ai
./server/bin/myteam config set server_url https://api.myteam.ai
./server/bin/myteam login
./server/bin/myteam daemon start
./server/bin/myteam runtime list`);
  });
});
