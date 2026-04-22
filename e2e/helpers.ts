import { type Page } from "@playwright/test";
import { TestApiClient } from "./fixtures";

export interface E2EIdentity {
  name: string;
  email: string;
  workspaceName: string;
  workspaceSlug: string;
}

export function createE2EIdentity(label = "default"): E2EIdentity {
  const suffix = `${label}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
  return {
    name: `E2E User ${label}`,
    email: `e2e+${suffix}@myteam.ai`,
    workspaceName: `E2E Workspace ${label}`,
    workspaceSlug: `e2e-workspace-${suffix}`,
  };
}

/**
 * Log in as the default E2E user and ensure the workspace exists first.
 * Authenticates via API (send-code → DB read → verify-code), then injects
 * the token into localStorage so the browser session is authenticated.
 */
export async function loginAsDefault(
  page: Page,
  identity: E2EIdentity = createE2EIdentity(),
) {
  const api = new TestApiClient();
  await api.login(identity.email, identity.name);
  const workspace = await api.ensureWorkspace(
    identity.workspaceName,
    identity.workspaceSlug,
  );
  const token = api.getToken();
  if (!token) {
    throw new Error(`Failed to obtain E2E token for ${identity.email}`);
  }
  await loginWithApi(page, token, workspace.id);
}

export async function loginWithApi(
  page: Page,
  token: string,
  workspaceId: string,
) {
  await page.goto("/login", { waitUntil: "domcontentloaded" });
  await page.evaluate(
    ({ nextToken, workspaceId }) => {
      localStorage.setItem("myteam_token", nextToken);
      localStorage.setItem("myteam_workspace_id", workspaceId);
    },
    { nextToken: token, workspaceId },
  );
  await page.goto("/projects", { waitUntil: "domcontentloaded" });
  await page.waitForURL("**/projects", { timeout: 10000 });
  await page.getByRole("button", { name: "创建项目" }).waitFor({
    state: "visible",
    timeout: 10000,
  });
}

/**
 * Create a TestApiClient logged in as the default E2E user.
 * Call api.cleanup() in afterEach to remove test data created during the test.
 */
export async function createTestApi(
  identity: E2EIdentity = createE2EIdentity(),
): Promise<TestApiClient> {
  const api = new TestApiClient();
  await api.login(identity.email, identity.name);
  await api.ensureWorkspace(identity.workspaceName, identity.workspaceSlug);
  return api;
}

export async function openWorkspaceMenu(page: Page) {
  const sidebar = page.locator('[data-slot="sidebar"]');
  await sidebar.waitFor({ state: "visible", timeout: 10000 });
  await sidebar.getByRole("button").first().click();
  await page.getByText("工作区").waitFor({ state: "visible" });
}
