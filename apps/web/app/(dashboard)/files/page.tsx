"use client"
import { useEffect, useState, useRef, useCallback } from "react"
import { useSearchParams, useRouter } from "next/navigation"
import { ChevronDown, ChevronRight, History, Download } from "lucide-react"
import { Button } from "@/components/ui/button"
import { api } from "@/shared/api"
import { toast } from "sonner"
import type { FileVersion } from "@/shared/types"
import { MemoriesTab } from "@/features/memories/components/memories-tab"
import { FileViewerPanel } from "@/features/messaging/components/file-viewer-panel"
import { useFileViewerStore } from "@/features/messaging/stores/file-viewer-store"

type Tab = "files" | "memories"

interface FileItem {
  id: string
  file_name: string
  file_size?: number
  content_type?: string
  url?: string
  source_type?: string
  created_at: string
}

const FILE_ICONS: Record<string, string> = {
  pdf: "📕", doc: "📘", docx: "📘", xls: "📗", xlsx: "📗", csv: "📊",
  png: "🖼️", jpg: "🖼️", jpeg: "🖼️", gif: "🖼️", svg: "🖼️",
  ts: "🟦", tsx: "🟦", js: "🟨", jsx: "🟨", py: "🐍", go: "🔵", rs: "🦀",
  zip: "📦", tar: "📦", gz: "📦", rar: "📦",
  md: "📝", txt: "📝", json: "📝", yaml: "📝", yml: "📝",
}

function getIcon(name: string) {
  const ext = name.split(".").pop()?.toLowerCase() ?? ""
  return FILE_ICONS[ext] ?? "📄"
}

function FileVersionHistory({ fileId }: { fileId: string }) {
  const [versions, setVersions] = useState<FileVersion[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError("")
    api
      .listFileVersions(fileId)
      .then((v) => {
        if (!cancelled) setVersions(v)
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : "Failed to load versions")
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => { cancelled = true }
  }, [fileId])

  if (loading) {
    return (
      <div className="pl-10 py-2 text-xs text-muted-foreground">
        Loading versions...
      </div>
    )
  }

  if (error) {
    return (
      <div className="pl-10 py-2 text-xs text-destructive">{error}</div>
    )
  }

  if (versions.length === 0) {
    return (
      <div className="pl-10 py-2 text-xs text-muted-foreground">
        No version history available.
      </div>
    )
  }

  return (
    <div className="pl-10 pb-2 space-y-1">
      {versions.map((v) => (
        <div
          key={v.id}
          className="flex items-center gap-3 px-3 py-1.5 rounded-md bg-muted/30 text-xs"
        >
          <span className="font-mono text-muted-foreground">v{v.version}</span>
          <span className="truncate flex-1">{v.filename}</span>
          <span className="text-muted-foreground shrink-0">
            {formatSize(v.size_bytes)}
          </span>
          <span className="text-muted-foreground shrink-0">
            {new Date(v.created_at).toLocaleString()}
          </span>
          {v.download_url && (
            <a
              href={v.download_url}
              target="_blank"
              rel="noopener noreferrer"
              className="shrink-0"
            >
              <Button variant="ghost" size="xs">
                <Download className="h-3 w-3" />
              </Button>
            </a>
          )}
        </div>
      ))}
    </div>
  )
}

function formatSize(bytes?: number) {
  if (!bytes) return ""
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1048576) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1048576).toFixed(1)} MB`
}

function timeAgo(dateStr: string) {
  const diff = Date.now() - new Date(dateStr).getTime()
  const min = Math.floor(diff / 60000)
  if (min < 1) return "刚刚"
  if (min < 60) return `${min} 分钟前`
  const hr = Math.floor(min / 60)
  if (hr < 24) return `${hr} 小时前`
  const d = Math.floor(hr / 24)
  if (d < 30) return `${d} 天前`
  return new Date(dateStr).toLocaleDateString()
}

export default function FilesPage() {
  const searchParams = useSearchParams()
  const router = useRouter()
  const tab: Tab = searchParams.get("tab") === "memories" ? "memories" : "files"

  const setTab = (next: Tab) => {
    const params = new URLSearchParams(searchParams.toString())
    if (next === "memories") params.set("tab", "memories")
    else params.delete("tab")
    const qs = params.toString()
    router.replace(qs ? `/files?${qs}` : "/files")
  }

  return (
    <div className="flex h-full flex-col bg-background">
      <div className="flex items-center gap-1 border-b border-border px-4 pt-3">
        <button
          type="button"
          onClick={() => setTab("files")}
          className={`px-3 py-2 text-[13px] font-medium border-b-2 transition-colors ${
            tab === "files"
              ? "border-primary text-foreground"
              : "border-transparent text-muted-foreground hover:text-foreground"
          }`}
        >
          文件 (RAW)
        </button>
        <button
          type="button"
          onClick={() => setTab("memories")}
          className={`px-3 py-2 text-[13px] font-medium border-b-2 transition-colors ${
            tab === "memories"
              ? "border-primary text-foreground"
              : "border-transparent text-muted-foreground hover:text-foreground"
          }`}
        >
          记忆 wiki
        </button>
      </div>
      <div className="flex min-h-0 flex-1 overflow-hidden">
        {tab === "files" ? <FilesTab /> : (
          <div className="flex min-h-0 flex-1 p-4">
            <MemoriesTab />
          </div>
        )}
      </div>
    </div>
  )
}

function FilesViewerPanel() {
  const activeFile = useFileViewerStore((s) => s.active)
  const closeFileViewer = useFileViewerStore((s) => s.close)
  if (!activeFile) return null
  return <FileViewerPanel target={activeFile} onClose={closeFileViewer} />
}

function FilesTab() {
  const [files, setFiles] = useState<FileItem[]>([])
  const [loading, setLoading] = useState(true)
  const [uploading, setUploading] = useState(false)
  const [expandedFileId, setExpandedFileId] = useState<string | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const openFile = useFileViewerStore((s) => s.open)
  const activeFileId = useFileViewerStore((s) => s.active?.file_id ?? null)

  const loadFiles = useCallback(async () => {
    try {
      // Try file_index API first
      const indexFiles = await api.listMyFiles().catch(() => null)
      if (Array.isArray(indexFiles) && indexFiles.length > 0) {
        setFiles(indexFiles.map((f: any) => ({
          id: f.id,
          file_name: f.file_name,
          file_size: f.file_size,
          content_type: f.content_type,
          url: f.storage_path,
          source_type: f.source_type,
          created_at: f.created_at,
        })))
        return
      }

      // Fallback: collect attachments from issues
      const res = await fetch("/api/issues?limit=50")
      if (!res.ok) { setFiles([]); return }
      const data = await res.json()
      const issues = Array.isArray(data) ? data : Array.isArray(data?.issues) ? data.issues : []
      const allFiles: FileItem[] = []
      for (const issue of issues) {
        if (issue.attachments?.length > 0) {
          for (const att of issue.attachments) {
            allFiles.push({
              id: att.id,
              file_name: att.file_name ?? att.filename ?? "unknown",
              file_size: att.file_size ?? att.size_bytes,
              content_type: att.file_content_type ?? att.content_type,
              url: att.url,
              source_type: "issue",
              created_at: att.created_at ?? issue.created_at,
            })
          }
        }
      }
      setFiles(allFiles)
    } catch {
      setFiles([])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { loadFiles() }, [loadFiles])

  async function handleUpload(e: React.ChangeEvent<HTMLInputElement>) {
    const fileList = e.target.files
    if (!fileList || fileList.length === 0) return
    setUploading(true)
    let successCount = 0
    try {
      for (const file of Array.from(fileList)) {
        try {
          await api.uploadFile(file)
          successCount++
        } catch (err) {
          const msg = err instanceof Error ? err.message : "上传失败"
          if (msg.includes("not configured") || msg.includes("unavailable")) {
            toast.error("文件上传服务未配置（需要 S3 存储）")
            break
          }
          toast.error(`上传 ${file.name} 失败: ${msg}`)
        }
      }
      if (successCount > 0) {
        toast.success(`已上传 ${successCount} 个文件`)
        await loadFiles()
      }
    } finally {
      setUploading(false)
      if (fileInputRef.current) fileInputRef.current.value = ""
    }
  }

  function toggleExpanded(fileId: string) {
    setExpandedFileId((prev) => (prev === fileId ? null : fileId))
  }

  return (
    <div className="flex flex-1 min-h-0 overflow-hidden">
      <div className="flex-1 overflow-auto p-6">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold text-foreground">文件 ({files.length})</h1>
        <div>
          <input ref={fileInputRef} type="file" multiple className="hidden" onChange={handleUpload} />
          <button onClick={() => fileInputRef.current?.click()} disabled={uploading}
            className="px-4 py-2 bg-primary text-primary-foreground rounded-[6px] text-sm font-medium disabled:opacity-50 hover:opacity-90 transition-opacity">
            {uploading ? "上传中..." : "上传文件"}
          </button>
        </div>
      </div>

      {loading && <div className="text-muted-foreground py-4">加载中...</div>}

      {!loading && files.length === 0 && (
        <div className="text-center py-16">
          <div className="text-4xl mb-3">📁</div>
          <p className="font-medium text-foreground">暂无文件</p>
          <p className="text-sm text-muted-foreground mt-1">在会话中发送的文件和上传的文件将显示在此处</p>
          <p className="text-xs text-muted-foreground mt-3 bg-secondary rounded-[6px] px-3 py-2 inline-block">
            💡 本地开发环境需要配置 S3 存储才能上传文件。设置 <code className="font-mono bg-muted px-1 rounded">S3_BUCKET</code> 环境变量。
          </p>
        </div>
      )}

      <div className="space-y-1">
        {files.map((f) => (
          <div key={f.id}>
            <div
              className={`flex items-center gap-3 p-3 rounded-[8px] border transition-colors cursor-pointer ${
                activeFileId === f.id
                  ? "border-primary bg-secondary/60"
                  : "border-border hover:bg-secondary/50"
              }`}
              onClick={() =>
                openFile({
                  file_id: f.id,
                  file_name: f.file_name,
                  file_size: f.file_size,
                  file_content_type: f.content_type,
                })
              }
            >
              <button
                className="shrink-0 text-muted-foreground hover:text-foreground"
                onClick={(e) => { e.stopPropagation(); toggleExpanded(f.id) }}
              >
                {expandedFileId === f.id ? (
                  <ChevronDown className="h-4 w-4" />
                ) : (
                  <ChevronRight className="h-4 w-4" />
                )}
              </button>
              <span className="text-2xl shrink-0">{getIcon(f.file_name)}</span>
              <div className="flex-1 min-w-0">
                <div className="font-medium truncate text-foreground text-[14px]">{f.file_name}</div>
                <div className="text-[12px] text-muted-foreground flex items-center gap-2">
                  {f.file_size ? <span>{formatSize(f.file_size)}</span> : null}
                  {f.content_type && <span>{f.content_type}</span>}
                  <span>{timeAgo(f.created_at)}</span>
                </div>
              </div>
              {f.source_type && (
                <span className="px-2 py-0.5 text-[11px] rounded-full border border-border text-secondary-foreground bg-secondary/50">
                  {f.source_type}
                </span>
              )}
              <Button
                variant="ghost"
                size="xs"
                className="shrink-0 text-muted-foreground hover:text-foreground"
                onClick={(e) => { e.stopPropagation(); toggleExpanded(f.id) }}
              >
                <History className="h-3.5 w-3.5" />
                Versions
              </Button>
              {f.url && (
                <a href={f.url} target="_blank" rel="noopener noreferrer"
                  className="px-3 py-1 text-[12px] rounded-[6px] border border-border text-secondary-foreground hover:bg-secondary/50 transition-colors"
                  onClick={(e) => e.stopPropagation()}>
                  下载
                </a>
              )}
            </div>

            {/* Version History (expanded) */}
            {expandedFileId === f.id && (
              <div className="border-x border-b rounded-b-lg -mt-px">
                <div className="flex items-center gap-1.5 px-4 pt-3 pb-1">
                  <History className="h-3.5 w-3.5 text-muted-foreground" />
                  <span className="text-xs font-medium text-muted-foreground">Version History</span>
                </div>
                <FileVersionHistory fileId={f.id} />
              </div>
            )}
          </div>
        ))}
      </div>
      </div>
      <FilesViewerPanel />
    </div>
  )
}
