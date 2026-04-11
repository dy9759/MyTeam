import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, act } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const mockPush = vi.fn();

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: mockPush }),
}));

const mockSearch = vi.fn();

vi.mock("@/shared/api", () => ({
  api: {
    search: (...args: any[]) => mockSearch(...args),
  },
}));

// Mock the cmdk-based command components to avoid internal store issues in jsdom
vi.mock("@/components/ui/command", () => ({
  CommandDialog: ({
    open,
    onOpenChange,
    children,
  }: {
    open: boolean;
    onOpenChange: (open: boolean) => void;
    title?: string;
    description?: string;
    children: React.ReactNode;
  }) => (open ? <div data-testid="command-dialog">{children}</div> : null),
  CommandInput: ({
    value,
    onValueChange,
    placeholder,
  }: {
    value: string;
    onValueChange: (v: string) => void;
    placeholder?: string;
  }) => (
    <input
      placeholder={placeholder}
      value={value}
      onChange={(e) => onValueChange(e.target.value)}
      data-testid="command-input"
    />
  ),
  CommandList: ({ children }: { children: React.ReactNode }) => (
    <div data-testid="command-list">{children}</div>
  ),
  CommandEmpty: ({ children }: { children: React.ReactNode }) => (
    <div data-testid="command-empty">{children}</div>
  ),
  CommandGroup: ({
    heading,
    children,
  }: {
    heading: string;
    children: React.ReactNode;
  }) => (
    <div data-testid="command-group">
      <div>{heading}</div>
      {children}
    </div>
  ),
  CommandItem: ({
    children,
    onSelect,
    value,
  }: {
    children: React.ReactNode;
    onSelect: () => void;
    value?: string;
  }) => (
    <div data-testid="command-item" onClick={onSelect} role="option">
      {children}
    </div>
  ),
  CommandSeparator: () => <hr />,
}));

import { CommandSearch } from "./command-search";

describe("CommandSearch", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.useFakeTimers({ shouldAdvanceTime: true });
    mockSearch.mockResolvedValue({ results: [], total: 0 });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("opens on Cmd+K", () => {
    render(<CommandSearch />);

    expect(screen.queryByTestId("command-dialog")).not.toBeInTheDocument();

    act(() => {
      document.dispatchEvent(
        new KeyboardEvent("keydown", {
          key: "k",
          metaKey: true,
          bubbles: true,
        })
      );
    });

    expect(screen.getByTestId("command-dialog")).toBeInTheDocument();
  });

  it("opens on Ctrl+K", () => {
    render(<CommandSearch />);

    act(() => {
      document.dispatchEvent(
        new KeyboardEvent("keydown", {
          key: "k",
          ctrlKey: true,
          bubbles: true,
        })
      );
    });

    expect(screen.getByTestId("command-dialog")).toBeInTheDocument();
  });

  it("shows search results grouped by type", async () => {
    mockSearch.mockResolvedValue({
      results: [
        {
          type: "issue",
          id: "i1",
          title: "Fix login bug",
          preview: "Auth error",
          score: 1,
        },
        {
          type: "agent",
          id: "a1",
          title: "Claude Bot",
          preview: "AI assistant",
          score: 0.9,
        },
        {
          type: "message",
          id: "m1",
          title: "Hello team",
          preview: "Discussion",
          score: 0.8,
        },
      ],
      total: 3,
    });

    render(<CommandSearch />);

    // Open dialog
    act(() => {
      document.dispatchEvent(
        new KeyboardEvent("keydown", {
          key: "k",
          metaKey: true,
          bubbles: true,
        })
      );
    });

    const input = screen.getByTestId("command-input");

    await act(async () => {
      await userEvent.type(input, "test", { delay: null });
    });

    // Advance debounce timer
    act(() => {
      vi.advanceTimersByTime(350);
    });

    await waitFor(() => {
      expect(mockSearch).toHaveBeenCalledWith("test");
    });

    await waitFor(() => {
      expect(screen.getByText("Fix login bug")).toBeInTheDocument();
      expect(screen.getByText("Claude Bot")).toBeInTheDocument();
      expect(screen.getByText("Hello team")).toBeInTheDocument();
    });

    // Check group headings
    expect(screen.getByText("Issues")).toBeInTheDocument();
    expect(screen.getByText("Agents")).toBeInTheDocument();
    expect(screen.getByText("Messages")).toBeInTheDocument();
  });

  it("navigates to issue on select", async () => {
    mockSearch.mockResolvedValue({
      results: [
        {
          type: "issue",
          id: "issue-123",
          title: "Fix login bug",
          preview: "",
          score: 1,
        },
      ],
      total: 1,
    });

    render(<CommandSearch />);

    act(() => {
      document.dispatchEvent(
        new KeyboardEvent("keydown", {
          key: "k",
          metaKey: true,
          bubbles: true,
        })
      );
    });

    const input = screen.getByTestId("command-input");
    await act(async () => {
      await userEvent.type(input, "fix", { delay: null });
    });

    act(() => {
      vi.advanceTimersByTime(350);
    });

    await waitFor(() => {
      expect(screen.getByText("Fix login bug")).toBeInTheDocument();
    });

    await act(async () => {
      await userEvent.click(screen.getByText("Fix login bug"));
    });

    expect(mockPush).toHaveBeenCalledWith("/issues/issue-123");
  });

  it("navigates to account for agent results", async () => {
    mockSearch.mockResolvedValue({
      results: [
        {
          type: "agent",
          id: "agent-1",
          title: "Claude Bot",
          preview: "",
          score: 1,
        },
      ],
      total: 1,
    });

    render(<CommandSearch />);

    act(() => {
      document.dispatchEvent(
        new KeyboardEvent("keydown", {
          key: "k",
          metaKey: true,
          bubbles: true,
        })
      );
    });

    const input = screen.getByTestId("command-input");
    await act(async () => {
      await userEvent.type(input, "claude", { delay: null });
    });

    act(() => {
      vi.advanceTimersByTime(350);
    });

    await waitFor(() => {
      expect(screen.getByText("Claude Bot")).toBeInTheDocument();
    });

    await act(async () => {
      await userEvent.click(screen.getByText("Claude Bot"));
    });

    expect(mockPush).toHaveBeenCalledWith("/account?tab=agents");
  });

  it("shows empty state when no results", async () => {
    mockSearch.mockResolvedValue({ results: [], total: 0 });

    render(<CommandSearch />);

    act(() => {
      document.dispatchEvent(
        new KeyboardEvent("keydown", {
          key: "k",
          metaKey: true,
          bubbles: true,
        })
      );
    });

    const input = screen.getByTestId("command-input");
    await act(async () => {
      await userEvent.type(input, "nonexistent", { delay: null });
    });

    act(() => {
      vi.advanceTimersByTime(350);
    });

    await waitFor(() => {
      expect(screen.getByText("No results found.")).toBeInTheDocument();
    });
  });
});
