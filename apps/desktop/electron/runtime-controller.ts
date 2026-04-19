import { execFile } from "node:child_process";
import path from "node:path";
import { promisify } from "node:util";
import type { SessionRuntime } from "@myteam/client-core";

const execFileAsync = promisify(execFile);

// CLI commands should return well under 10s. Longer means daemon is
// hung (port collision, db unreachable). Killing here prevents zombie
// processes accumulating across renderer reconnects. Issue #45.
const CLI_TIMEOUT_MS = 10_000;

export class DesktopRuntimeController {
  constructor(
    private readonly projectRoot: string,
    private readonly serverUrl: string,
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
    return {
      cwd: this.projectRoot,
      env: this.baseEnv(),
      timeout: CLI_TIMEOUT_MS,
      killSignal: "SIGKILL" as const,
    };
  }

  async startDaemon(): Promise<void> {
    await execFileAsync(this.getCliPath(), ["daemon", "start"], this.execOpts());
  }

  async stopDaemon(): Promise<void> {
    await execFileAsync(this.getCliPath(), ["daemon", "stop"], this.execOpts());
  }

  async listRuntimes(): Promise<SessionRuntime[]> {
    const { stdout } = await execFileAsync(
      this.getCliPath(),
      ["runtime", "list", "--output", "json"],
      this.execOpts(),
    );
    return JSON.parse(stdout) as SessionRuntime[];
  }

  async watchWorkspace(workspaceId: string): Promise<void> {
    await execFileAsync(
      this.getCliPath(),
      ["workspace", "watch", workspaceId],
      this.execOpts(),
    );
  }
}
