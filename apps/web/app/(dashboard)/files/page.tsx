"use client"
import { useEffect, useState, useRef } from "react"

export default function FilesPage() {
  const [files, setFiles] = useState<any[]>([])
  const [loading, setLoading] = useState(true)
  const [uploading, setUploading] = useState(false)
  const fileInputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    // Fetch files from issues' attachments
    async function load() {
      try {
        // Use issues API to get attachments (no direct file list API)
        // For now fetch recent issues and collect attachments
        const res = await fetch("/api/issues?limit=50")
        const data = await res.json()
        const allFiles: any[] = []
        // Files will appear as attachments from issues
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
      // Reload
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
  function formatSize(bytes: number) {
    if (!bytes) return ""
    if (bytes < 1024) return `${bytes} B`
    if (bytes < 1048576) return `${(bytes/1024).toFixed(1)} KB`
    return `${(bytes/1048576).toFixed(1)} MB`
  }

  return (
    <div className="p-6 bg-background min-h-full">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold text-foreground">Files ({files.length})</h1>
        <div>
          <input ref={fileInputRef} type="file" multiple className="hidden" onChange={handleUpload} />
          <button onClick={() => fileInputRef.current?.click()} disabled={uploading}
            className="px-4 py-2 bg-brand text-brand-foreground rounded-md text-sm disabled:opacity-50 hover:opacity-90 transition-opacity">
            {uploading ? "Uploading..." : "Upload File"}
          </button>
        </div>
      </div>

      {loading && <div className="text-muted-foreground">Loading...</div>}

      {!loading && files.length === 0 && (
        <div className="text-center py-16 text-[#8a8f98]">
          <div className="text-4xl mb-3">📁</div>
          <p className="font-medium text-[#d0d6e0]">No files yet</p>
          <p className="text-sm mt-1">Files shared in channels, tasks, and uploads will appear here.</p>
        </div>
      )}

      <div className="space-y-1">
        {files.map((f: any) => (
          <div key={f.id} className="flex items-center gap-3 p-3 rounded-lg border border-[rgba(255,255,255,0.05)] hover:bg-[rgba(255,255,255,0.03)] transition-colors">
            <span className="text-2xl">{getIcon(f.filename ?? "")}</span>
            <div className="flex-1 min-w-0">
              <div className="font-medium truncate text-foreground">{f.filename}</div>
              <div className="text-xs text-muted-foreground">
                {formatSize(f.size_bytes)} · {f.content_type} · {new Date(f.created_at).toLocaleDateString()}
              </div>
            </div>
            <span className="px-2 py-0.5 text-xs rounded-full bg-[rgba(255,255,255,0.06)] text-[#d0d6e0]">
              {f.source_type ?? "upload"}
            </span>
            <a href={f.url} target="_blank" rel="noopener noreferrer"
              className="px-3 py-1 text-sm rounded border border-[rgba(255,255,255,0.08)] text-[#d0d6e0] hover:bg-[rgba(255,255,255,0.05)] transition-colors">
              Download
            </a>
          </div>
        ))}
      </div>
    </div>
  )
}
