"use client";

import { useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import {
  CommandDialog,
  CommandInput,
  CommandList,
  CommandEmpty,
  CommandGroup,
  CommandItem,
  CommandSeparator,
} from "@/components/ui/command";
import { api } from "@/shared/api";
import type { SearchResult } from "@/shared/types";
import {
  CircleDotIcon,
  BotIcon,
  MessageSquareIcon,
  FileIcon,
  Loader2Icon,
} from "lucide-react";

function useDebounce<T>(value: T, delay: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delay);
    return () => clearTimeout(timer);
  }, [value, delay]);
  return debounced;
}

const TYPE_ICONS: Record<string, React.ElementType> = {
  issue: CircleDotIcon,
  agent: BotIcon,
  message: MessageSquareIcon,
  file: FileIcon,
};

const TYPE_LABELS: Record<string, string> = {
  issue: "Issues",
  agent: "Agents",
  message: "Messages",
  file: "Files",
};

function groupByType(results: SearchResult[]): Record<string, SearchResult[]> {
  const groups: Record<string, SearchResult[]> = {};
  for (const r of results) {
    const list = groups[r.type];
    if (list) {
      list.push(r);
    } else {
      groups[r.type] = [r];
    }
  }
  return groups;
}

export function CommandSearch() {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<SearchResult[]>([]);
  const [loading, setLoading] = useState(false);
  const debouncedQuery = useDebounce(query, 300);

  // Keyboard shortcut: Cmd+K / Ctrl+K
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key === "k") {
        e.preventDefault();
        setOpen((prev) => !prev);
      }
    }
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, []);

  // Fetch search results when debounced query changes
  useEffect(() => {
    if (!debouncedQuery.trim()) {
      setResults([]);
      setLoading(false);
      return;
    }

    let cancelled = false;
    setLoading(true);

    api
      .search(debouncedQuery)
      .then((data) => {
        if (!cancelled) {
          setResults(data.results ?? []);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setResults([]);
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [debouncedQuery]);

  // Reset state when dialog closes
  const handleOpenChange = useCallback((nextOpen: boolean) => {
    setOpen(nextOpen);
    if (!nextOpen) {
      setQuery("");
      setResults([]);
      setLoading(false);
    }
  }, []);

  function navigateToResult(result: SearchResult) {
    setOpen(false);
    switch (result.type) {
      case "issue":
        router.push(`/issues/${result.id}`);
        break;
      case "agent":
        router.push("/account?tab=agents");
        break;
      case "message":
        router.push("/session");
        break;
      case "file":
        router.push("/files");
        break;
    }
  }

  const grouped = groupByType(results);
  const groupKeys = Object.keys(grouped);

  return (
    <CommandDialog
      open={open}
      onOpenChange={handleOpenChange}
      title="Search"
      description="Search across issues, agents, and messages"
    >
      <CommandInput
        placeholder="Search issues, agents, messages..."
        value={query}
        onValueChange={setQuery}
      />
      <CommandList>
        {loading && (
          <div className="flex items-center justify-center py-6">
            <Loader2Icon className="size-4 animate-spin text-muted-foreground" />
            <span className="ml-2 text-sm text-muted-foreground">
              Searching...
            </span>
          </div>
        )}

        {!loading && debouncedQuery.trim() && results.length === 0 && (
          <CommandEmpty>No results found.</CommandEmpty>
        )}

        {!loading &&
          groupKeys.map((type, idx) => {
            const Icon = TYPE_ICONS[type] ?? FileIcon;
            const label = TYPE_LABELS[type] ?? type;
            const items = grouped[type] ?? [];
            return (
              <div key={type}>
                {idx > 0 && <CommandSeparator />}
                <CommandGroup heading={label}>
                  {items.map((result) => (
                    <CommandItem
                      key={`${result.type}-${result.id}`}
                      value={`${result.type}-${result.id}-${result.title}`}
                      onSelect={() => navigateToResult(result)}
                    >
                      <Icon className="size-4 shrink-0 text-muted-foreground" />
                      <div className="flex flex-col gap-0.5 overflow-hidden">
                        <span className="truncate text-sm">{result.title}</span>
                        {result.preview && result.preview !== result.title && (
                          <span className="truncate text-xs text-muted-foreground">
                            {result.preview}
                          </span>
                        )}
                      </div>
                    </CommandItem>
                  ))}
                </CommandGroup>
              </div>
            );
          })}

        {!loading && !debouncedQuery.trim() && (
          <CommandEmpty>Type to search across your workspace.</CommandEmpty>
        )}
      </CommandList>
    </CommandDialog>
  );
}
