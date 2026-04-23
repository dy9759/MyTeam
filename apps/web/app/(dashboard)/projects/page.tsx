"use client";

import { useEffect, useState, useMemo } from "react";
import { useProjectStore } from "@/features/projects";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Plus, Loader2, FolderOpen, Search } from "lucide-react";
import type { Project, ProjectStatus, ProjectScheduleType } from "@/shared/types";
import { CreateProjectDialog } from "@/features/projects/components/create-project-dialog";
import ProjectDetailInline from "./[id]/page";

const STATUS_BADGE: Record<ProjectStatus, string> = {
  not_started: "bg-accent text-muted-foreground border-border",
  draft: "bg-accent text-muted-foreground border-border",
  scheduled: "bg-[rgba(94,106,210,0.15)] text-[#8b9cf7] border-[rgba(94,106,210,0.25)]",
  running: "bg-[rgba(94,106,210,0.15)] text-[#8b9cf7] border-[rgba(94,106,210,0.25)]",
  paused: "bg-[rgba(255,180,50,0.15)] text-[#f0b440] border-[rgba(255,180,50,0.25)]",
  completed: "bg-[rgba(39,166,68,0.15)] text-[#4ade80] border-[rgba(39,166,68,0.25)]",
  failed: "bg-[rgba(239,68,68,0.15)] text-[#f87171] border-[rgba(239,68,68,0.25)]",
  stopped: "bg-accent text-muted-foreground/60 border-border",
  archived: "bg-accent text-muted-foreground/60 border-border",
};

const STATUS_LABEL: Record<ProjectStatus, string> = {
  not_started: "未开始",
  draft: "草稿",
  scheduled: "已调度",
  running: "运行中",
  paused: "已暂停",
  completed: "已完成",
  failed: "失败",
  stopped: "已停止",
  archived: "已归档",
};

const SCHEDULE_LABEL: Record<ProjectScheduleType, string> = {
  one_time: "一次性",
  scheduled: "定时",
  scheduled_once: "定时",
  recurring: "周期性",
};

export default function ProjectsPage() {
  const { projects, loading, fetch } = useProjectStore();
  const [createOpen, setCreateOpen] = useState(false);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState("");

  useEffect(() => {
    fetch();
  }, [fetch]);

  const filteredProjects = useMemo(() => {
    if (!searchQuery.trim()) return projects;
    const q = searchQuery.toLowerCase();
    return projects.filter(
      (p) =>
        p.title.toLowerCase().includes(q) ||
        p.description?.toLowerCase().includes(q)
    );
  }, [projects, searchQuery]);

  return (
    <div className="flex flex-1 min-h-0">
      {/* Left sidebar */}
      <div className="flex w-[280px] shrink-0 flex-col border-r border-border bg-card">
        {/* Search */}
        <div className="p-3 border-b border-border">
          <div className="relative">
            <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5 text-muted-foreground/60" />
            <Input
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="搜索项目..."
              className="pl-8 h-8 text-sm bg-accent border-border text-foreground placeholder:text-muted-foreground/60"
            />
          </div>
        </div>

        {/* Project list */}
        <div className="flex-1 min-h-0 overflow-y-auto">
          {loading ? (
            <div className="flex items-center justify-center py-12">
              <Loader2 className="size-5 animate-spin text-muted-foreground" />
            </div>
          ) : filteredProjects.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-12 text-muted-foreground px-4">
              <p className="text-sm">
                {searchQuery ? "无匹配项目" : "暂无项目"}
              </p>
            </div>
          ) : (
            <div className="p-1.5 space-y-0.5">
              {filteredProjects.map((project: Project) => (
                <button
                  key={project.id}
                  type="button"
                  className={`w-full text-left rounded-md px-3 py-2.5 transition-colors ${
                    selectedId === project.id
                      ? "bg-muted text-foreground"
                      : "hover:bg-accent text-secondary-foreground"
                  }`}
                  onClick={() => setSelectedId(project.id)}
                >
                  <div className="font-medium text-sm truncate">
                    {project.title}
                  </div>
                  <div className="flex items-center gap-2 mt-1">
                    <Badge
                      className={`text-[10px] px-1.5 py-0 ${STATUS_BADGE[project.status] ?? ""}`}
                      variant="outline"
                    >
                      {STATUS_LABEL[project.status] ?? project.status}
                    </Badge>
                    <span className="text-[11px] text-muted-foreground">
                      {SCHEDULE_LABEL[project.schedule_type]}
                    </span>
                  </div>
                </button>
              ))}
            </div>
          )}
        </div>

        {/* Create button */}
        <div className="p-3 border-t border-border">
          <Button
            onClick={() => setCreateOpen(true)}
            className="w-full bg-primary hover:bg-primary/90 text-white"
            size="sm"
          >
            <Plus className="size-3.5 mr-1.5" />
            创建项目
          </Button>
        </div>
      </div>

      {/* Right area: detail or empty state */}
      <div className="flex-1 min-h-0 overflow-hidden">
        {selectedId ? (
          <ProjectDetailInline projectId={selectedId} />
        ) : (
          <div className="flex flex-col items-center justify-center h-full text-muted-foreground gap-2">
            <FolderOpen className="size-10 text-muted-foreground/60" />
            <p className="text-sm">选择一个项目</p>
          </div>
        )}
      </div>

      {createOpen && (
        <CreateProjectDialog
          onClose={() => setCreateOpen(false)}
          onCreated={(project) => {
            setCreateOpen(false);
            setSelectedId(project.id);
          }}
        />
      )}
    </div>
  );
}
