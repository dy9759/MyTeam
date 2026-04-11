import { execFile } from "node:child_process";
import { promisify } from "node:util";
import path from "node:path";

const execFileAsync = promisify(execFile);

type NativeCommand =
  | ["keychain.get"]
  | ["keychain.set", string]
  | ["keychain.delete"]
  | ["notification.show", string, string]
  | ["file.open", string]
  | ["file.reveal", string]
  | ["file.openPanel"]
  | ["bookmark.store", string]
  | ["bookmark.resolve", string];

export class NativeBridge {
  constructor(private readonly projectRoot: string) {}

  private swiftPackageDir() {
    return path.join(this.projectRoot, "apps/desktop/native-macos");
  }

  private async run<T>(...command: NativeCommand): Promise<T> {
    const { stdout } = await execFileAsync("swift", ["run", "MyTeamNative", ...command], {
      cwd: this.swiftPackageDir(),
      env: process.env,
    });
    const output = stdout.trim();
    if (!output) {
      return undefined as T;
    }
    return JSON.parse(output) as T;
  }

  async getToken(): Promise<string | null> {
    const response = await this.run<{ token: string | null }>("keychain.get");
    return response.token ?? null;
  }

  async setToken(token: string): Promise<void> {
    await this.run("keychain.set", token);
  }

  async deleteToken(): Promise<void> {
    await this.run("keychain.delete");
  }

  async showNotification(title: string, body: string): Promise<void> {
    await this.run("notification.show", title, body);
  }

  async openPath(targetPath: string): Promise<void> {
    await this.run("file.open", targetPath);
  }

  async revealPath(targetPath: string): Promise<void> {
    await this.run("file.reveal", targetPath);
  }

  async openPanel(): Promise<string[]> {
    const response = await this.run<{ paths: string[] }>("file.openPanel");
    return response.paths ?? [];
  }
}
