"use client";

import { useEffect, useMemo, useState } from "react";
import { ExternalLink, Pencil, Save, X, Eye, RefreshCw } from "lucide-react";
import { toast } from "sonner";

import { api } from "@/shared/api";
import type { FileVersion } from "@/shared/types";
import { formatSize } from "@/shared/file-display";
import type { FileViewerTarget } from "@/features/messaging/stores/file-viewer-store";

import { detectKind, parseCsv, typeBadge } from "./file-viewer/utils";
import { ViewerBody } from "./file-viewer/viewer-body";

export { detectKind, parseCsv };

interface FileViewerPanelProps {
  target: FileViewerTarget;
  onClose: () => void;
}

export function FileViewerPanel({ target, onClose }: FileViewerPanelProps) {
  const ext = target.file_name.split(".").pop()?.toLowerCase() ?? "";
  const kind = useMemo(() => detectKind(target.file_name, target.file_content_type), [target]);
  const editable = kind === "markdown" || kind === "html" || kind === "text" || kind === "code" || kind === "csv";

  const [version, setVersion] = useState<FileVersion | null>(null);
  const [versionsErr, setVersionsErr] = useState<string>("");
  const [content, setContent] = useState<string>("");
  const [contentLoaded, setContentLoaded] = useState(false);
  const [contentErr, setContentErr] = useState<string>("");
  const [loading, setLoading] = useState(true);
  const [mode, setMode] = useState<"preview" | "edit">("preview");
  const [draft, setDraft] = useState<string>("");
  const [reloadTick, setReloadTick] = useState(0);

  // Authenticated binary content. `blobUrl` is a local object URL fed to
  // <img>/<iframe>; `sheets` holds parsed xlsx rows per sheet for inline
  // table rendering.
  const [blobUrl, setBlobUrl] = useState<string | null>(null);
  const [binaryErr, setBinaryErr] = useState<string>("");
  const [sheets, setSheets] = useState<{ name: string; rows: string[][] }[] | null>(null);
  const [activeSheetIdx, setActiveSheetIdx] = useState(0);

  // Fetch latest FileVersion → need download_url for rendering + loading text content.
  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setVersion(null);
    setContent("");
    setContentLoaded(false);
    setContentErr("");
    setVersionsErr("");
    setBinaryErr("");
    setSheets(null);
    setActiveSheetIdx(0);
    setMode("preview");

    api
      .listFileVersions(target.file_id)
      .then((raw) => {
        if (cancelled) return;
        const list: FileVersion[] = Array.isArray(raw)
          ? (raw as FileVersion[])
          : Array.isArray((raw as { versions?: FileVersion[] })?.versions)
            ? ((raw as { versions: FileVersion[] }).versions)
            : [];
        if (list.length === 0) {
          setVersionsErr("No versions available for this file.");
          setLoading(false);
          return;
        }
        const latest = [...list].sort((a, b) => (b.version ?? 0) - (a.version ?? 0))[0];
        setVersion(latest ?? null);
        setLoading(false);
      })
      .catch((e) => {
        if (cancelled) return;
        setVersionsErr(e instanceof Error ? e.message : "Failed to load file");
        setLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [target.file_id, reloadTick]);

  // Binary fetch — image / pdf / office / excel all go through the authed
  // download proxy. A blob object-URL feeds <img>/<iframe>; for xlsx we
  // additionally run the bytes through SheetJS to render the first sheet
  // inline as an HTML table.
  useEffect(() => {
    if (!version) return;
    if (kind !== "image" && kind !== "pdf" && kind !== "office" && kind !== "excel") {
      return;
    }
    let cancelled = false;
    let createdUrl: string | null = null;
    (async () => {
      try {
        if (kind === "excel") {
          const buf = await api.downloadFileArrayBuffer(target.file_id);
          if (cancelled) return;
          const xlsx = await import("xlsx");
          const wb = xlsx.read(buf, { type: "array" });
          const parsed = wb.SheetNames.map((name) => {
            const ws = wb.Sheets[name];
            const rows = ws ? (xlsx.utils.sheet_to_json(ws, { header: 1, defval: "" }) as unknown[][]) : [];
            return {
              name,
              rows: rows.map((r) => r.map((c) => (c == null ? "" : String(c)))),
            };
          });
          if (!cancelled) setSheets(parsed);
          return;
        }
        const blob = await api.downloadFileBlob(target.file_id);
        if (cancelled) return;
        createdUrl = URL.createObjectURL(blob);
        setBlobUrl(createdUrl);
      } catch (e) {
        if (cancelled) return;
        setBinaryErr(e instanceof Error ? e.message : "Failed to load file");
      }
    })();
    return () => {
      cancelled = true;
      if (createdUrl) URL.revokeObjectURL(createdUrl);
      setBlobUrl(null);
    };
  }, [version, kind, target.file_id]);

  // Fetch text content for editable/text-like kinds. Goes through the
  // authenticated /api/files/:id/download proxy instead of the raw
  // object-store URL so browsers that can't reach the bucket directly
  // (no public read, no CDN signing in dev) still render correctly.
  useEffect(() => {
    if (!version) return;
    const textual = kind === "markdown" || kind === "html" || kind === "text" || kind === "code" || kind === "csv";
    if (!textual) {
      setContentLoaded(true);
      return;
    }
    let cancelled = false;
    api
      .downloadFileText(target.file_id)
      .then((t) => {
        if (cancelled) return;
        setContent(t);
        setDraft(t);
        setContentLoaded(true);
      })
      .catch((e) => {
        if (cancelled) return;
        setContentErr(e instanceof Error ? e.message : "Failed to read file content");
        setContentLoaded(true);
      });
    return () => {
      cancelled = true;
    };
  }, [version, kind, target.file_id]);

  const openExternal = () => {
    if (version?.download_url) window.open(version.download_url, "_blank", "noopener,noreferrer");
  };

  const handleSave = () => {
    // Backend lacks an endpoint to create a new FileVersion for the same file_id.
    // Keep the local edit in the panel so the user can still iterate, and surface
    // the gap honestly instead of pretending the change persisted.
    setContent(draft);
    setMode("preview");
    toast.message("已在本地更新（服务器端保存尚未接入）", {
      description: "刷新或切换文件后本地修改会丢失。",
    });
  };

  const handleReload = () => {
    setReloadTick((n) => n + 1);
  };

  return (
    <div className="w-[520px] max-w-[60vw] border-l border-border flex flex-col h-full bg-card">
      {/* Header */}
      <div className="px-4 py-3 border-b border-border flex items-center justify-between gap-2 shrink-0">
        <div className="flex items-center gap-2 min-w-0">
          <span className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-secondary text-secondary-foreground shrink-0">
            {typeBadge(kind, ext)}
          </span>
          <h3 className="font-medium text-[14px] text-foreground truncate" title={target.file_name}>
            {target.file_name}
          </h3>
          {target.file_size != null && (
            <span className="text-[11px] text-muted-foreground shrink-0">
              {formatSize(target.file_size)}
            </span>
          )}
          {version && (
            <span className="text-[11px] text-muted-foreground shrink-0">
              v{version.version}
            </span>
          )}
        </div>
        <div className="flex items-center gap-0.5 shrink-0">
          {editable && contentLoaded && !contentErr && (
            mode === "preview" ? (
              <button
                type="button"
                onClick={() => {
                  setDraft(content);
                  setMode("edit");
                }}
                className="p-1 rounded-[4px] hover:bg-secondary text-muted-foreground hover:text-foreground transition-colors"
                title="编辑"
                aria-label="编辑"
              >
                <Pencil className="h-4 w-4" />
              </button>
            ) : (
              <>
                <button
                  type="button"
                  onClick={handleSave}
                  className="p-1 rounded-[4px] hover:bg-secondary text-muted-foreground hover:text-foreground transition-colors"
                  title="保存"
                  aria-label="保存"
                >
                  <Save className="h-4 w-4" />
                </button>
                <button
                  type="button"
                  onClick={() => {
                    setDraft(content);
                    setMode("preview");
                  }}
                  className="p-1 rounded-[4px] hover:bg-secondary text-muted-foreground hover:text-foreground transition-colors"
                  title="预览"
                  aria-label="预览"
                >
                  <Eye className="h-4 w-4" />
                </button>
              </>
            )
          )}
          <button
            type="button"
            onClick={handleReload}
            className="p-1 rounded-[4px] hover:bg-secondary text-muted-foreground hover:text-foreground transition-colors"
            title="重新加载"
            aria-label="重新加载"
          >
            <RefreshCw className="h-4 w-4" />
          </button>
          <button
            type="button"
            onClick={openExternal}
            disabled={!version?.download_url}
            className="p-1 rounded-[4px] hover:bg-secondary text-muted-foreground hover:text-foreground transition-colors disabled:opacity-40"
            title="在新标签页打开"
            aria-label="在新标签页打开"
          >
            <ExternalLink className="h-4 w-4" />
          </button>
          <button
            type="button"
            onClick={onClose}
            className="p-1 rounded-[4px] hover:bg-secondary text-muted-foreground hover:text-foreground transition-colors"
            title="关闭"
            aria-label="关闭"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
      </div>

      {/* Body */}
      <div className="flex-1 min-h-0 overflow-auto">
        {loading && (
          <div className="h-full flex items-center justify-center text-[13px] text-muted-foreground">
            加载中...
          </div>
        )}
        {!loading && versionsErr && (
          <div className="p-4 text-[13px] text-destructive">{versionsErr}</div>
        )}
        {!loading && version && (
          <ViewerBody
            kind={kind}
            version={version}
            content={content}
            draft={draft}
            setDraft={setDraft}
            contentLoaded={contentLoaded}
            contentErr={contentErr}
            mode={mode}
            blobUrl={blobUrl}
            binaryErr={binaryErr}
            sheets={sheets}
            activeSheetIdx={activeSheetIdx}
            setActiveSheetIdx={setActiveSheetIdx}
          />
        )}
      </div>
    </div>
  );
}
