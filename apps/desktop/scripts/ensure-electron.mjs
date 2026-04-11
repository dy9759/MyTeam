import fs from "node:fs";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);

function resolveElectronDir() {
  const packageJsonPath = require.resolve("electron/package.json", {
    paths: [process.cwd()],
  });
  return path.dirname(packageJsonPath);
}

const electronDir = resolveElectronDir();
const pathFile = path.join(electronDir, "path.txt");

if (fs.existsSync(pathFile)) {
  process.exit(0);
}

const installScript = path.join(electronDir, "install.js");
if (!fs.existsSync(installScript)) {
  console.error("[desktop] Electron install script is missing.");
  process.exit(1);
}

console.warn("[desktop] Electron binary is missing. Repairing the local install...");

const result = spawnSync(process.execPath, [installScript], {
  cwd: electronDir,
  env: process.env,
  stdio: "inherit",
});

if (result.status !== 0) {
  process.exit(result.status ?? 1);
}

if (!fs.existsSync(pathFile)) {
  console.error("[desktop] Electron install did not create path.txt.");
  process.exit(1);
}

console.log("[desktop] Electron install repaired.");
