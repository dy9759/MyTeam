import { execFile } from "node:child_process";
import path from "node:path";
import { promisify } from "node:util";
import type { SessionRuntime } from "@myteam/client-core";

const execFileAsync = promisify(execFile);

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

  async startDaemon(): Promise<void> {
    await execFileAsync(this.getCliPath(), ["daemon", "start"], {
      cwd: this.projectRoot,
      env: this.baseEnv(),
    });
  }

  async stopDaemon(): Promise<void> {
    await execFileAsync(this.getCliPath(), ["daemon", "stop"], {
      cwd: this.projectRoot,
      env: this.baseEnv(),
    });
  }

  async listRuntimes(): Promise<SessionRuntime[]> {
    const { stdout } = await execFileAsync(
      this.getCliPath(),
      ["runtime", "list", "--output", "json"],
      {
        cwd: this.projectRoot,
        env: this.baseEnv(),
      },
    );
    return JSON.parse(stdout) as SessionRuntime[];
  }

  async watchWorkspace(workspaceId: string): Promise<void> {
    await execFileAsync(
      this.getCliPath(),
      ["workspace", "watch", workspaceId],
      {
        cwd: this.projectRoot,
        env: this.baseEnv(),
      },
    );
  }
}
