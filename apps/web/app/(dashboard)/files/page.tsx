"use client"
import { useEffect, useState, useRef } from "react"
import { ChevronDown, ChevronRight, History, Download } from "lucide-react"
import { Button } from "@/components/ui/button"
import { api } from "@/shared/api"
import type { FileVersion } from "@/shared/types"

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

function formatSize(bytes: number) {
  if (!bytes) return ""
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1048576) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1048576).toFixed(1)} MB`
}

export default function FilesPage() {
  const [files, setFiles] = useState<any[]>([])
  const [loading, setLoading] = useState(true)
  const [uploading, setUploading] = useState(false)
  const [expandedFileId, setExpandedFileId] = useState<string | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    async function load() {
      try {
        const res = await fetch("/api/issues?limit=50")
        const data = await res.json()
        const allFiles: any[] = []
        setFiles(allFiles)
      } catch {} finally { setLoading(false) }
    }
    load()
  }, [])

  async function handleUpload(e: React.ChangeEvent<HTMLInputElement>) {
    const fileList = e.target.files
    if (!fileList || fileList.length === 0) return
    setUploading(true)
    try {
      for (const file of Array.from(fileList)) {
        const formData = new FormData()
        formData.append("file", file)
        await fetch("/api/upload-file", { method: "POST", body: formData })
      }
    } catch {} finally {
      setUploading(false)
      if (fileInputRef.current) fileInputRef.current.value = ""
    }
  }

  const fileIcons: Record<string, string> = {
    pdf: "📕", doc: "📘", xls: "📗", png: "🖼️", jpg: "🖼️",
    ts: "🟦", js: "🟨", py: "🐍", go: "🔵", zip: "📦"
  }
  function getIcon(name: string) {
    const ext = name.split(".").pop()?.toLowerCase() ?? ""
    return fileIcons[ext] ?? "📄"
  }

  function toggleExpanded(fileId: string) {
    setExpandedFileId((prev) => (prev === fileId ? null : fileId))
  }

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold">Files ({files.length})</h1>
        <div>
          <input ref={fileInputRef} type="file" multiple className="hidden" onChange={handleUpload} />
          <button onClick={() => fileInputRef.current?.click()} disabled={uploading}
            className="px-4 py-2 bg-primary text-primary-foreground rounded-md text-sm disabled:opacity-50">
            {uploading ? "Uploading..." : "Upload File"}
          </button>
        </div>
      </div>

      {loading && <div className="text-muted-foreground">Loading...</div>}

      {!loading && files.length === 0 && (
        <div className="text-center py-16 text-muted-foreground">
          <div className="text-4xl mb-3">📁</div>
          <p className="font-medium">No files yet</p>
          <p className="text-sm mt-1">Files shared in channels, tasks, and uploads will appear here.</p>
        </div>
      )}

      <div className="space-y-1">
        {files.map((f: any) => (
          <div key={f.id}>
            <div
              className="flex items-center gap-3 p-3 border rounded-lg hover:bg-muted/50 cursor-pointer"
              onClick={() => toggleExpanded(f.id)}
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
              <span className="text-2xl">{getIcon(f.filename ?? "")}</span>
              <div className="flex-1 min-w-0">
                <div className="font-medium truncate">{f.filename}</div>
                <div className="text-xs text-muted-foreground">
                  {formatSize(f.size_bytes)} · {f.content_type} · {new Date(f.created_at).toLocaleDateString()}
                </div>
              </div>
              <Button
                variant="ghost"
                size="xs"
                className="shrink-0 text-muted-foreground hover:text-foreground"
                onClick={(e) => { e.stopPropagation(); toggleExpanded(f.id) }}
              >
                <History className="h-3.5 w-3.5" />
                Versions
              </Button>
              <a href={f.url} target="_blank" rel="noopener noreferrer"
                className="px-3 py-1 text-sm bg-primary/10 text-primary rounded hover:bg-primary/20"
                onClick={(e) => e.stopPropagation()}>
                Download
              </a>
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
  )
}
