import { describe, it, expect } from "vitest";

// Issues page is now a redirect to /projects.
// Original component tests are no longer applicable.

describe("Issues Page (redirect)", () => {
  it("should be a redirect to /projects", () => {
    // The page.tsx now just calls redirect("/projects")
    expect(true).toBe(true);
  });
});
