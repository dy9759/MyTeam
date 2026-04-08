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
  not_started: "bg-[rgba(255,255,255,0.06)] text-[#8a8f98] border-[rgba(255,255,255,0.08)]",
  running: "bg-[rgba(94,106,210,0.15)] text-[#8b9cf7] border-[rgba(94,106,210,0.25)]",
  paused: "bg-[rgba(255,180,50,0.15)] text-[#f0b440] border-[rgba(255,180,50,0.25)]",
  completed: "bg-[rgba(39,166,68,0.15)] text-[#4ade80] border-[rgba(39,166,68,0.25)]",
  failed: "bg-[rgba(239,68,68,0.15)] text-[#f87171] border-[rgba(239,68,68,0.25)]",
  archived: "bg-[rgba(255,255,255,0.06)] text-[#62666d] border-[rgba(255,255,255,0.08)]",
};

const STATUS_LABEL: Record<ProjectStatus, string> = {
  not_started: "未开始",
  running: "运行中",
  paused: "已暂停",
  completed: "已完成",
  failed: "失败",
  archived: "已归档",
};

const SCHEDULE_LABEL: Record<ProjectScheduleType, string> = {
  one_time: "一次性",
  scheduled: "定时",
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
      <div className="flex w-[280px] shrink-0 flex-col border-r border-[rgba(255,255,255,0.05)] bg-[#0f1011]">
        {/* Search */}
        <div className="p-3 border-b border-[rgba(255,255,255,0.05)]">
          <div className="relative">
            <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5 text-[#62666d]" />
            <Input
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="搜索项目..."
              className="pl-8 h-8 text-sm bg-[rgba(255,255,255,0.05)] border-[rgba(255,255,255,0.08)] text-[#f7f8f8] placeholder:text-[#62666d]"
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
            <div className="flex flex-col items-center justify-center py-12 text-[#8a8f98] px-4">
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
                      ? "bg-[rgba(255,255,255,0.08)] text-[#f7f8f8]"
                      : "hover:bg-[rgba(255,255,255,0.05)] text-[#d0d6e0]"
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
                    <span className="text-[11px] text-[#8a8f98]">
                      {SCHEDULE_LABEL[project.schedule_type]}
                    </span>
                  </div>
                </button>
              ))}
            </div>
          )}
        </div>

        {/* Create button */}
        <div className="p-3 border-t border-[rgba(255,255,255,0.05)]">
          <Button
            onClick={() => setCreateOpen(true)}
            className="w-full bg-[#5e6ad2] hover:bg-[#4f5abf] text-white"
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
          <div className="flex flex-col items-center justify-center h-full text-[#8a8f98] gap-2">
            <FolderOpen className="size-10 text-[#62666d]" />
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
