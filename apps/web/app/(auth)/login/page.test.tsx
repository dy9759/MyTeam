import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

// Mock next/navigation
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn() }),
  usePathname: () => "/login",
  useSearchParams: () => new URLSearchParams(),
}));

// Mock auth store
const mockSendCode = vi.fn();
const mockVerifyCode = vi.fn();
vi.mock("@/features/auth", () => ({
  useAuthStore: (selector: (s: any) => any) =>
    selector({
      sendCode: mockSendCode,
      verifyCode: mockVerifyCode,
    }),
}));

// Mock workspace store
const mockHydrateWorkspace = vi.fn();
vi.mock("@/features/workspace", () => ({
  useWorkspaceStore: (selector: (s: any) => any) =>
    selector({
      hydrateWorkspace: mockHydrateWorkspace,
    }),
}));

// Mock api
vi.mock("@/shared/api", () => ({
  api: {
    listWorkspaces: vi.fn().mockResolvedValue([]),
    verifyCode: vi.fn(),
    setToken: vi.fn(),
    getMe: vi.fn(),
  },
}));

import LoginPage from "./page";

describe("LoginPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders login form with email input and continue button", () => {
    render(<LoginPage />);

    expect(screen.getByText("My Team")).toBeInTheDocument();
    expect(screen.getByLabelText("邮箱")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "继续" })
    ).toBeInTheDocument();
  });

  it("does not call sendCode when email is empty", async () => {
    const user = userEvent.setup();
    render(<LoginPage />);

    // Form has required attribute so empty submit won't fire
    await user.click(screen.getByRole("button", { name: "继续" }));
    expect(mockSendCode).not.toHaveBeenCalled();
  });

  it("calls sendCode with email on submit", async () => {
    mockSendCode.mockResolvedValueOnce(undefined);
    const user = userEvent.setup();
    render(<LoginPage />);

    await user.type(screen.getByLabelText("邮箱"), "test@myteam.ai");
    await user.click(screen.getByRole("button", { name: "继续" }));

    await waitFor(() => {
      expect(mockSendCode).toHaveBeenCalledWith("test@myteam.ai");
    });
  });

  it("shows submitting state while sending code", async () => {
    mockSendCode.mockReturnValueOnce(new Promise(() => {}));
    const user = userEvent.setup();
    render(<LoginPage />);

    await user.type(screen.getByLabelText("邮箱"), "test@myteam.ai");
    await user.click(screen.getByRole("button", { name: "继续" }));

    await waitFor(() => {
      expect(screen.getByText("发送验证码中...")).toBeInTheDocument();
    });
  });

  it("shows verification code step after sending code", async () => {
    mockSendCode.mockResolvedValueOnce(undefined);
    const user = userEvent.setup();
    render(<LoginPage />);

    await user.type(screen.getByLabelText("邮箱"), "test@myteam.ai");
    await user.click(screen.getByRole("button", { name: "继续" }));

    await waitFor(() => {
      // After sending code, the component should show the verification view
      expect(screen.getByText(/验证码/)).toBeInTheDocument();
    });
  });

  it("shows error when sendCode fails", async () => {
    mockSendCode.mockRejectedValueOnce(new Error("Network error"));
    const user = userEvent.setup();
    render(<LoginPage />);

    await user.type(screen.getByLabelText("邮箱"), "test@myteam.ai");
    await user.click(screen.getByRole("button", { name: "继续" }));

    await waitFor(() => {
      expect(screen.getByText("Network error")).toBeInTheDocument();
    });
  });
});
