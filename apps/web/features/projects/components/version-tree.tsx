"use client";

import { Badge } from "@/components/ui/badge";
import type { ProjectVersion } from "@/shared/types";
import { GitBranch } from "lucide-react";

interface VersionTreeProps {
  versions: ProjectVersion[];
  currentVersionId?: string;
  onSelect: (version: ProjectVersion) => void;
}

export function VersionTree({ versions, currentVersionId, onSelect }: VersionTreeProps) {
  // Sort by version_number
  const sorted = [...versions].sort((a, b) => a.version_number - b.version_number);

  // Build tree: group by parent_version_id
  function getDepth(version: ProjectVersion): number {
    if (!version.parent_version_id) return 0;
    const parent = versions.find((v) => v.id === version.parent_version_id);
    return parent ? getDepth(parent) + 1 : 0;
  }

  if (sorted.length === 0) {
    return (
      <div className="text-sm text-muted-foreground text-center py-4">
        暂无版本记录
      </div>
    );
  }

  return (
    <div className="space-y-1">
      {sorted.map((version) => {
        const depth = getDepth(version);
        const isCurrent = version.id === currentVersionId;

        return (
          <div
            key={version.id}
            className={`flex items-center gap-2 p-2 rounded-md cursor-pointer transition-colors ${isCurrent ? "bg-primary/10 ring-1 ring-primary" : "hover:bg-accent/50"}`}
            style={{ paddingLeft: `${depth * 24 + 8}px` }}
            onClick={() => onSelect(version)}
          >
            <GitBranch className="size-3.5 text-muted-foreground shrink-0" />
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium">v{version.version_number}</span>
                {version.branch_name && (
                  <Badge variant="outline" className="text-xs">
                    {version.branch_name}
                  </Badge>
                )}
                {isCurrent && (
                  <Badge variant="secondary" className="text-xs">
                    当前
                  </Badge>
                )}
                {version.version_status === "archived" && (
                  <Badge variant="outline" className="text-xs text-muted-foreground">
                    已归档
                  </Badge>
                )}
              </div>
              {version.fork_reason && (
                <div className="text-xs text-muted-foreground mt-0.5 truncate">
                  {version.fork_reason}
                </div>
              )}
              <div className="text-xs text-muted-foreground">
                {new Date(version.created_at).toLocaleDateString()}
              </div>
            </div>
          </div>
        );
      })}
    </div>
  );
}
