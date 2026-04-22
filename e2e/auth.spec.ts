import { test, expect } from "@playwright/test";
import { loginAsDefault, openWorkspaceMenu } from "./helpers";

test.describe("Authentication", () => {
  test("login page renders correctly", async ({ page }) => {
    await page.goto("/login");

    await expect(page.getByText("My Team")).toBeVisible();
    await expect(page.getByRole("textbox", { name: "邮箱" })).toBeVisible();
    await expect(page.locator('button[type="submit"]')).toContainText(
      "继续",
    );
  });

  test("login and redirect to /projects", async ({ page }) => {
    await loginAsDefault(page);

    await expect(page).toHaveURL(/\/projects/);
    await expect(page.getByRole("button", { name: "创建项目" })).toBeVisible();
  });

  test("unauthenticated user is redirected to /login", async ({ page }) => {
    await page.goto("/login");
    await page.evaluate(() => {
      localStorage.removeItem("myteam_token");
      localStorage.removeItem("myteam_workspace_id");
    });

    await page.goto("/projects");
    await page.waitForURL("**/login", { timeout: 10000 });
  });

  test("logout redirects to /login", async ({ page }) => {
    await loginAsDefault(page);

    // Open the workspace dropdown menu
    await openWorkspaceMenu(page);

    await page.getByText("退出登录").click();

    await page.waitForURL("**/login", { timeout: 10000 });
    await expect(page).toHaveURL(/\/login/);
  });
});
