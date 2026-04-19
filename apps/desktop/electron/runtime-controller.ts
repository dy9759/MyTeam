import { execFile } from "node:child_process";
import path from "node:path";
import { promisify } from "node:util";
import type { SessionRuntime } from "@myteam/client-core";

const execFileAsync = promisify(execFile);

// CLI commands should return well under 10s. Longer means daemon is
// hung (port collision, db unreachable). Killing here prevents zombie
// processes accumulating across renderer reconnects. Issue #45.
const CLI_TIMEOUT_MS = 10_000;

export function resolveRuntimeInvocation(projectRoot: string, devMode: boolean) {
  if (devMode) {
    return {
      command: "go",
      argsPrefix: ["run", "./cmd/myteam"],
      cwd: path.join(projectRoot, "server"),
    };
  }

  return {
    command: path.join(projectRoot, "server/bin/myteam"),
    argsPrefix: [] as string[],
    cwd: projectRoot,
  };
}

export class DesktopRuntimeController {
  constructor(
    private readonly projectRoot: string,
    private readonly serverUrl: string,
    private readonly devMode = false,
  ) {}

  getCliPath() {
    return path.join(this.projectRoot, "server/bin/myteam");
  }

  private baseEnv() {
    return {
      ...process.env,
      MYTEAM_SERVER_URL: this.serverUrl,
    };
  }

  // execOpts centralizes timeout + cwd + env so every call honors the
  // same kill deadline.
  private execOpts() {
    const invocation = resolveRuntimeInvocation(this.projectRoot, this.devMode);
    return {
      cwd: invocation.cwd,
      env: this.baseEnv(),
      timeout: CLI_TIMEOUT_MS,
      killSignal: "SIGKILL" as const,
    };
  }

  private invocation() {
    return resolveRuntimeInvocation(this.projectRoot, this.devMode);
  }

  private async runCli(args: string[]) {
    const invocation = this.invocation();
    await execFileAsync(
      invocation.command,
      [...invocation.argsPrefix, ...args],
      this.execOpts(),
    );
  }

  async startDaemon(): Promise<void> {
    await this.runCli(["daemon", "start"]);
  }

  async stopDaemon(): Promise<void> {
    await this.runCli(["daemon", "stop"]);
  }

  async listRuntimes(): Promise<SessionRuntime[]> {
    const invocation = this.invocation();
    const { stdout } = await execFileAsync(
      invocation.command,
      [...invocation.argsPrefix, "runtime", "list", "--output", "json"],
      this.execOpts(),
    );
    return JSON.parse(stdout) as SessionRuntime[];
  }

  async watchWorkspace(workspaceId: string): Promise<void> {
    await this.runCli(["workspace", "watch", workspaceId]);
  }
}
