"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { useProjectStore } from "@/features/projects";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Plus, Loader2 } from "lucide-react";
import type { Project, ProjectStatus, ProjectScheduleType } from "@/shared/types";
import { CreateProjectDialog } from "@/features/projects/components/create-project-dialog";

const STATUS_BADGE: Record<ProjectStatus, string> = {
  not_started: "bg-muted text-muted-foreground",
  running: "bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200",
  paused: "bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200",
  completed: "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200",
  failed: "bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200",
  archived: "bg-muted text-muted-foreground",
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
  const router = useRouter();
  const { projects, loading, fetch } = useProjectStore();
  const [createOpen, setCreateOpen] = useState(false);

  useEffect(() => {
    fetch();
  }, [fetch]);

  return (
    <div className="flex-1 overflow-auto p-6">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-semibold">项目</h1>
        <Button onClick={() => setCreateOpen(true)}>
          <Plus className="size-4 mr-2" />
          创建项目
        </Button>
      </div>

      {loading ? (
        <div className="flex items-center justify-center py-20">
          <Loader2 className="size-6 animate-spin text-muted-foreground" />
        </div>
      ) : projects.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-20 text-muted-foreground">
          <p className="text-lg mb-2">暂无项目</p>
          <p className="text-sm">创建一个新项目开始协作。</p>
        </div>
      ) : (
        <div className="space-y-2">
          {projects.map((project: Project) => (
            <div
              key={project.id}
              className="flex items-center gap-4 p-4 border rounded-lg cursor-pointer hover:bg-accent/50 transition-colors"
              onClick={() => router.push(`/projects/${project.id}`)}
            >
              <div className="flex-1 min-w-0">
                <div className="font-medium truncate">{project.title}</div>
                {project.description && (
                  <div className="text-sm text-muted-foreground truncate mt-0.5">
                    {project.description}
                  </div>
                )}
                <div className="text-xs text-muted-foreground mt-1">
                  {SCHEDULE_LABEL[project.schedule_type]} · {new Date(project.created_at).toLocaleDateString()}
                </div>
              </div>
              <Badge className={STATUS_BADGE[project.status] ?? ""} variant="outline">
                {STATUS_LABEL[project.status] ?? project.status}
              </Badge>
            </div>
          ))}
        </div>
      )}

      {createOpen && (
        <CreateProjectDialog
          onClose={() => setCreateOpen(false)}
          onCreated={(project) => {
            setCreateOpen(false);
            router.push(`/projects/${project.id}`);
          }}
        />
      )}
    </div>
  );
}
