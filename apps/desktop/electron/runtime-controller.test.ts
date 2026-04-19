import path from "node:path";
import { describe, expect, it } from "vitest";
import { resolveRuntimeInvocation } from "./runtime-controller";

describe("resolveRuntimeInvocation", () => {
  const projectRoot = "/tmp/myteam";

  it("uses go run from the server directory in development", () => {
    expect(resolveRuntimeInvocation(projectRoot, true)).toEqual({
      command: "go",
      argsPrefix: ["run", "./cmd/myteam"],
      cwd: path.join(projectRoot, "server"),
    });
  });

  it("uses the built CLI binary outside development", () => {
    expect(resolveRuntimeInvocation(projectRoot, false)).toEqual({
      command: path.join(projectRoot, "server/bin/myteam"),
      argsPrefix: [],
      cwd: projectRoot,
    });
  });
});
