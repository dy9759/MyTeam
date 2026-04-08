"use client";

import { useEffect, useState } from "react";
import { useFileStore } from "@/features/files";
import { useAuthStore } from "@/features/auth";
import { useWorkspaceStore } from "@/features/workspace";
import { api } from "@/shared/api";
import type { FileIndex, Project } from "@/shared/types";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Badge } from "@/components/ui/badge";
import { buttonVariants } from "@/components/ui/button";
import { Download, FileText } from "lucide-react";

// --- Helpers ---

function getFileIcon(fileName: string): string {
  const ext = fileName.split(".").pop()?.toLowerCase() ?? "";
  const map: Record<string, string> = {
    pdf: "\uD83D\uDCD5",
    doc: "\uD83D\uDCD8",
    docx: "\uD83D\uDCD8",
    xls: "\uD83D\uDCCA",
    xlsx: "\uD83D\uDCCA",
    csv: "\uD83D\uDCCA",
    png: "\uD83D\uDDBC\uFE0F",
    jpg: "\uD83D\uDDBC\uFE0F",
    jpeg: "\uD83D\uDDBC\uFE0F",
    gif: "\uD83D\uDDBC\uFE0F",
    svg: "\uD83D\uDDBC\uFE0F",
    zip: "\uD83D\uDCE6",
    tar: "\uD83D\uDCE6",
    gz: "\uD83D\uDCE6",
    rar: "\uD83D\uDCE6",
    ts: "\uD83D\uDCDD",
    tsx: "\uD83D\uDCDD",
    js: "\uD83D\uDCDD",
    jsx: "\uD83D\uDCDD",
    py: "\uD83D\uDCDD",
    go: "\uD83D\uDCDD",
    rs: "\uD83D\uDCDD",
    md: "\uD83D\uDCDD",
    txt: "\uD83D\uDCDD",
    json: "\uD83D\uDCDD",
    yaml: "\uD83D\uDCDD",
    yml: "\uD83D\uDCDD",
  };
  return map[ext] ?? "\uD83D\uDCC4";
}

function formatFileSize(bytes?: number): string {
  if (!bytes) return "";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}

function relativeTime(dateStr: string): string {
  const now = Date.now();
  const then = new Date(dateStr).getTime();
  const diff = now - then;
  const seconds = Math.floor(diff / 1000);
  if (seconds < 60) return "just now";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes} minutes ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} hours ago`;
  const days = Math.floor(hours / 24);
  if (days < 30) return `${days} days ago`;
  return new Date(dateStr).toLocaleDateString();
}

function sourceBadgeLabel(sourceType: string): string {
  switch (sourceType) {
    case "conversation":
      return "conversation";
    case "project":
      return "project";
    case "external":
      return "external";
    default:
      return sourceType;
  }
}

// --- File Row Component ---

function FileRow({ file }: { file: FileIndex }) {
  return (
    <div className="flex items-center gap-3 p-3 border rounded-lg hover:bg-muted/50 transition-colors">
      <span className="text-2xl shrink-0">{getFileIcon(file.file_name)}</span>
      <div className="flex-1 min-w-0">
        <div className="font-medium truncate">{file.file_name}</div>
        <div className="flex items-center gap-2 text-xs text-muted-foreground mt-0.5 flex-wrap">
          {file.file_size != null && (
            <span>{formatFileSize(file.file_size)}</span>
          )}
          <Badge variant="outline" className="text-xs py-0">
            {sourceBadgeLabel(file.source_type)}
          </Badge>
          {file.uploader_identity_type === "agent" && (
            <Badge variant="secondary" className="text-xs py-0">
              Agent
            </Badge>
          )}
          <span>{relativeTime(file.created_at)}</span>
        </div>
      </div>
      <a
        href={file.storage_path ?? "#"}
        target="_blank"
        rel="noopener noreferrer"
        className={buttonVariants({ variant: "outline", size: "sm" })}
      >
        <Download className="h-3.5 w-3.5 mr-1" />
        download
      </a>
    </div>
  );
}

function EmptyState({ message }: { message: string }) {
  return (
    <div className="text-center py-16 text-muted-foreground">
      <FileText className="h-10 w-10 mx-auto mb-3 opacity-50" />
      <p className="font-medium">{message}</p>
    </div>
  );
}

// --- Main Page ---

export default function FilesPage() {
  const { myFiles, loading, activeTab, setActiveTab, fetchMyFiles } =
    useFileStore();
  const user = useAuthStore((s) => s.user);
  const agents = useWorkspaceStore((s) => s.agents);

  const [allFiles, setAllFiles] = useState<FileIndex[]>([]);
  const [allLoading, setAllLoading] = useState(false);
  const [allFetched, setAllFetched] = useState(false);
  const [projects, setProjects] = useState<Project[]>([]);
  const [projectFilesMap, setProjectFilesMap] = useState<
    Record<string, FileIndex[]>
  >({});
  const [projectLoading, setProjectLoading] = useState(false);
  const [projectFetched, setProjectFetched] = useState(false);

  // Fetch my files on mount
  useEffect(() => {
    fetchMyFiles();
  }, [fetchMyFiles]);

  // Fetch all files when switching to agent tab (once)
  useEffect(() => {
    if (activeTab === "agent" && !allFetched && !allLoading) {
      setAllLoading(true);
      api
        .listMyFiles()
        .then((files) => {
          const arr = Array.isArray(files) ? files : [];
          setAllFiles(arr);
        })
        .catch(() => {})
        .finally(() => { setAllLoading(false); setAllFetched(true); });
    }
  }, [activeTab, allFetched, allLoading]);

  // Fetch project files when switching to project tab (once)
  useEffect(() => {
    if (activeTab === "project" && !projectFetched && !projectLoading) {
      setProjectLoading(true);
      api
        .listProjects()
        .then(async (data) => {
          const projs = Array.isArray(data) ? data : [];
          setProjects(projs);
          const map: Record<string, FileIndex[]> = {};
          for (const p of projs) {
            try {
              const files = await api.listProjectFiles(p.id);
              const arr = Array.isArray(files) ? files : [];
              if (arr.length > 0) map[p.id] = arr;
            } catch {
              // skip
            }
          }
          setProjectFilesMap(map);
        })
        .catch(() => {})
        .finally(() => { setProjectLoading(false); setProjectFetched(true); });
    }
  }, [activeTab, projectFetched, projectLoading]);

  // Partition files
  const myAgentIds = new Set(
    agents
      .filter((a) => a.owner_id === user?.id)
      .map((a) => a.id),
  );

  const ownerFiles = myFiles.filter(
    (f) => f.uploader_identity_type === "member" && f.owner_id === user?.id,
  );

  const agentFiles = (activeTab === "agent" ? allFiles : myFiles).filter(
    (f) =>
      f.uploader_identity_type === "agent" &&
      myAgentIds.has(f.uploader_identity_id),
  );

  const totalFileCount = myFiles.length;

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold">
          files ({totalFileCount})
        </h1>
      </div>

      <Tabs
        value={activeTab}
        onValueChange={(v) => setActiveTab(v as "my" | "agent" | "project")}
      >
        <TabsList>
          <TabsTrigger value="my">my files</TabsTrigger>
          <TabsTrigger value="agent">Agent files</TabsTrigger>
          <TabsTrigger value="project">project files</TabsTrigger>
        </TabsList>

        {/* My Files Tab */}
        <TabsContent value="my">
          {loading && (
            <div className="text-muted-foreground py-4">loading...</div>
          )}
          {!loading && ownerFiles.length === 0 && (
            <EmptyState message="no files yet" />
          )}
          <div className="space-y-2 mt-4">
            {ownerFiles.map((f) => (
              <FileRow key={f.id} file={f} />
            ))}
          </div>
        </TabsContent>

        {/* Agent Files Tab */}
        <TabsContent value="agent">
          {allLoading && (
            <div className="text-muted-foreground py-4">loading...</div>
          )}
          {!allLoading && agentFiles.length === 0 && (
            <EmptyState message="your Agent has no files yet" />
          )}
          <div className="space-y-2 mt-4">
            {agentFiles.map((f) => {
              const agentName =
                agents.find((a) => a.id === f.uploader_identity_id)?.name ??
                f.uploader_identity_id.slice(0, 8);
              return (
                <div key={f.id}>
                  <FileRow file={f} />
                  <div className="ml-11 text-xs text-muted-foreground -mt-1 mb-1">
                    uploaded by {agentName}
                  </div>
                </div>
              );
            })}
          </div>
        </TabsContent>

        {/* Project Files Tab */}
        <TabsContent value="project">
          {projectLoading && (
            <div className="text-muted-foreground py-4">loading...</div>
          )}
          {!projectLoading && Object.keys(projectFilesMap).length === 0 && (
            <EmptyState message="no project files yet" />
          )}
          <div className="space-y-6 mt-4">
            {projects
              .filter((p) => projectFilesMap[p.id]?.length)
              .map((project) => (
                <div key={project.id}>
                  <h3 className="font-semibold text-sm mb-2">
                    {project.title}
                    <Badge variant="outline" className="ml-2 text-xs">
                      {project.status}
                    </Badge>
                  </h3>
                  <div className="space-y-2">
                    {projectFilesMap[project.id]!.map((f) => (
                      <FileRow key={f.id} file={f} />
                    ))}
                  </div>
                </div>
              ))}
          </div>
        </TabsContent>
      </Tabs>
    </div>
  );
}
