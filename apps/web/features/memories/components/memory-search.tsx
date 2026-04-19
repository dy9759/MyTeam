"use client";

import { useState, type FormEvent } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { useMemoryStore } from "@/features/memories/store";

export function MemorySearch() {
  const [query, setQuery] = useState("");
  const search = useMemoryStore((state) => state.search);
  const searchResults = useMemoryStore((state) => state.searchResults);
  const searchLoading = useMemoryStore((state) => state.searchLoading);
  const searchError = useMemoryStore((state) => state.searchError);

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    void search({ query, top_k: 8 });
  };

  return (
    <aside className="flex w-80 shrink-0 flex-col border border-border bg-card">
      <div className="border-b border-border p-3">
        <h2 className="font-medium text-foreground">Search</h2>
        <p className="mt-1 text-sm text-muted-foreground">
          Find memory chunks by semantic match.
        </p>
      </div>

      <form className="flex gap-2 border-b border-border p-3" onSubmit={onSubmit}>
        <Input
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="Search memories"
          aria-label="Search memories"
        />
        <Button type="submit" disabled={searchLoading}>
          {searchLoading ? "Searching..." : "Search"}
        </Button>
      </form>

      <div className="min-h-0 flex-1 space-y-2 overflow-y-auto p-3">
        {searchError ? (
          <div className="rounded-md border border-border p-3 text-sm text-destructive">
            {searchError}
          </div>
        ) : searchResults.length === 0 ? (
          <div className="rounded-md border border-border p-3 text-sm text-muted-foreground">
            No search results yet.
          </div>
        ) : (
          searchResults.map((hit) => (
            <Card key={hit.chunk.id} size="sm">
              <CardContent className="space-y-2">
                <div className="flex items-center justify-between gap-2">
                  <Badge variant="outline">Score {hit.score.toFixed(2)}</Badge>
                  <span className="truncate text-xs text-muted-foreground">
                    {hit.chunk.memory_id}
                  </span>
                </div>
                <p className="text-sm leading-relaxed text-foreground">
                  {hit.chunk.text}
                </p>
              </CardContent>
            </Card>
          ))
        )}
      </div>
    </aside>
  );
}
