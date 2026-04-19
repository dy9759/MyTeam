"use client";

import { useEffect, useMemo, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useMemoryStore } from "@/features/memories/store";
import type { MemoryFilter, MemoryStatus } from "@/features/memories/api";

const MEMORY_TYPES = ["episode", "fact", "instruction", "summary"] as const;
const MEMORY_SCOPES = ["meeting", "global", "user"] as const;
const MEMORY_STATUSES = ["candidate", "confirmed", "archived"] as const;
const ALL_VALUE = "all";

type FilterValue = (typeof ALL_VALUE) | string;

function filterValue(value: FilterValue): string | undefined {
  return value === ALL_VALUE ? undefined : value;
}

function statusVariant(status: MemoryStatus): "default" | "outline" | "secondary" {
  if (status === "confirmed") return "default";
  if (status === "archived") return "outline";
  return "secondary";
}

export function MemoryList() {
  const memories = useMemoryStore((state) => state.memories);
  const loading = useMemoryStore((state) => state.loading);
  const error = useMemoryStore((state) => state.error);
  const fetchAll = useMemoryStore((state) => state.fetchAll);
  const promote = useMemoryStore((state) => state.promote);
  const [type, setType] = useState<FilterValue>(ALL_VALUE);
  const [scope, setScope] = useState<FilterValue>(ALL_VALUE);
  const [status, setStatus] = useState<FilterValue>(ALL_VALUE);
  const [promotingId, setPromotingId] = useState<string | null>(null);

  const filter = useMemo<MemoryFilter>(
    () => ({
      type: filterValue(type),
      scope: filterValue(scope),
      status: filterValue(status) as MemoryStatus | undefined,
      limit: 50,
      offset: 0,
    }),
    [scope, status, type],
  );

  useEffect(() => {
    void fetchAll(filter);
  }, [fetchAll, filter]);

  const onPromote = async (id: string) => {
    setPromotingId(id);
    await promote(id);
    await fetchAll(filter);
    setPromotingId(null);
  };

  return (
    <section className="flex min-w-0 flex-1 flex-col border border-border bg-card">
      <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-border p-3">
        <Select value={type} onValueChange={(value) => setType(value ?? ALL_VALUE)}>
          <SelectTrigger size="sm" className="w-36">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL_VALUE}>All types</SelectItem>
            {MEMORY_TYPES.map((item) => (
              <SelectItem key={item} value={item}>
                {item}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Select value={scope} onValueChange={(value) => setScope(value ?? ALL_VALUE)}>
          <SelectTrigger size="sm" className="w-36">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL_VALUE}>All scopes</SelectItem>
            {MEMORY_SCOPES.map((item) => (
              <SelectItem key={item} value={item}>
                {item}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Select value={status} onValueChange={(value) => setStatus(value ?? ALL_VALUE)}>
          <SelectTrigger size="sm" className="w-40">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL_VALUE}>All statuses</SelectItem>
            {MEMORY_STATUSES.map((item) => (
              <SelectItem key={item} value={item}>
                {item}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="min-h-0 flex-1 overflow-auto">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Status</TableHead>
              <TableHead>Type</TableHead>
              <TableHead>Scope</TableHead>
              <TableHead>Summary</TableHead>
              <TableHead>Tags</TableHead>
              <TableHead className="text-right">Action</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {loading ? (
              <TableRow>
                <TableCell colSpan={6} className="h-24 text-center text-muted-foreground">
                  Loading memories...
                </TableCell>
              </TableRow>
            ) : error ? (
              <TableRow>
                <TableCell colSpan={6} className="h-24 text-center text-destructive">
                  {error}
                </TableCell>
              </TableRow>
            ) : memories.length === 0 ? (
              <TableRow>
                <TableCell colSpan={6} className="h-24 text-center text-muted-foreground">
                  No memories found.
                </TableCell>
              </TableRow>
            ) : (
              memories.map((memory) => (
                <TableRow key={memory.id}>
                  <TableCell>
                    <Badge variant={statusVariant(memory.status)}>
                      {memory.status}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-muted-foreground">{memory.type}</TableCell>
                  <TableCell className="text-muted-foreground">{memory.scope}</TableCell>
                  <TableCell className="max-w-md">
                    <div className="overflow-hidden text-ellipsis whitespace-nowrap">
                      {memory.summary || memory.body || "Untitled memory"}
                    </div>
                  </TableCell>
                  <TableCell>
                    <div className="flex max-w-56 flex-wrap gap-1">
                      {memory.tags.length > 0 ? (
                        memory.tags.map((tag) => (
                          <Badge key={tag} variant="outline">
                            {tag}
                          </Badge>
                        ))
                      ) : (
                        <span className="text-muted-foreground">No tags</span>
                      )}
                    </div>
                  </TableCell>
                  <TableCell className="text-right">
                    {memory.status === "candidate" ? (
                      <Button
                        type="button"
                        size="sm"
                        variant="secondary"
                        disabled={promotingId === memory.id}
                        onClick={() => void onPromote(memory.id)}
                      >
                        {promotingId === memory.id ? "Promoting..." : "Promote"}
                      </Button>
                    ) : null}
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>
    </section>
  );
}
