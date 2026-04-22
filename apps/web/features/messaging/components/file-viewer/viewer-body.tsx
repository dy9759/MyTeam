"use client";

import { Download } from "lucide-react";

import type { FileVersion } from "@/shared/types";
import { MemoizedMarkdown } from "@/components/markdown";

import { CsvTable } from "./csv-table";
import { ExcelSheet } from "./excel-sheet";
import type { Kind } from "./utils";

interface ViewerBodyProps {
  kind: Kind;
  version: FileVersion;
  content: string;
  draft: string;
  setDraft: (v: string) => void;
  contentLoaded: boolean;
  contentErr: string;
  mode: "preview" | "edit";
  blobUrl: string | null;
  binaryErr: string;
  sheets: { name: string; rows: string[][] }[] | null;
  activeSheetIdx: number;
  setActiveSheetIdx: (i: number) => void;
}

export function ViewerBody({
  kind,
  version,
  content,
  draft,
  setDraft,
  contentLoaded,
  contentErr,
  mode,
  blobUrl,
  binaryErr,
  sheets,
  activeSheetIdx,
  setActiveSheetIdx,
}: ViewerBodyProps) {
  if (kind === "image") {
    if (binaryErr) return <div className="p-4 text-[13px] text-destructive">{binaryErr}</div>;
    if (!blobUrl) return <div className="h-full flex items-center justify-center text-[13px] text-muted-foreground">加载图片中...</div>;
    return (
      <div className="p-4 flex items-center justify-center">
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          src={blobUrl}
          alt={version.filename}
          className="max-w-full max-h-[85vh] object-contain rounded border border-border"
        />
      </div>
    );
  }

  if (kind === "pdf") {
    if (binaryErr) return <div className="p-4 text-[13px] text-destructive">{binaryErr}</div>;
    if (!blobUrl) return <div className="h-full flex items-center justify-center text-[13px] text-muted-foreground">加载 PDF 中...</div>;
    return (
      <iframe
        src={blobUrl}
        className="w-full h-full border-0"
        title={version.filename}
      />
    );
  }

  if (kind === "excel") {
    if (binaryErr) return <div className="p-4 text-[13px] text-destructive">{binaryErr}</div>;
    if (!sheets) return <div className="h-full flex items-center justify-center text-[13px] text-muted-foreground">解析表格中...</div>;
    return (
      <ExcelSheet sheets={sheets} activeIdx={activeSheetIdx} onSelect={setActiveSheetIdx} />
    );
  }

  if (kind === "office") {
    if (binaryErr) {
      return (
        <div className="p-4 text-[13px] text-destructive">
          {binaryErr}
        </div>
      );
    }
    return (
      <div className="h-full flex flex-col">
        <div className="px-4 py-2 text-[12px] text-muted-foreground bg-secondary/40 border-b border-border flex items-center gap-2">
          <span>Office 文件暂无内嵌预览，浏览器将尝试下载。</span>
          {blobUrl && (
            <a
              href={blobUrl}
              download={version.filename}
              className="inline-flex items-center gap-1 text-primary hover:underline"
            >
              <Download className="h-3 w-3" /> 下载
            </a>
          )}
        </div>
        {blobUrl ? (
          <iframe src={blobUrl} className="flex-1 w-full border-0" title={version.filename} />
        ) : (
          <div className="flex-1 flex items-center justify-center text-[13px] text-muted-foreground">加载中...</div>
        )}
      </div>
    );
  }

  if (!contentLoaded) {
    return (
      <div className="h-full flex items-center justify-center text-[13px] text-muted-foreground">
        加载内容中...
      </div>
    );
  }

  if (contentErr) {
    return <div className="p-4 text-[13px] text-destructive">{contentErr}</div>;
  }

  if (mode === "edit") {
    return (
      <textarea
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        spellCheck={false}
        className="w-full h-full p-4 bg-background text-foreground font-mono text-[13px] leading-relaxed outline-none resize-none"
      />
    );
  }

  if (kind === "markdown") {
    return (
      <div className="p-4 prose prose-sm max-w-none dark:prose-invert">
        <MemoizedMarkdown mode="full">{content}</MemoizedMarkdown>
      </div>
    );
  }

  if (kind === "html") {
    // srcDoc so the HTML runs in an isolated document without a network
    // round-trip. sandbox="allow-scripts" lets inline <script> execute for
    // real fidelity (diagrams, charts) while blocking access to the parent
    // app — the iframe is cross-origin and can't read cookies or fire
    // credentialed requests back to our API.
    return (
      <iframe
        title={version.filename}
        srcDoc={content}
        sandbox="allow-scripts"
        className="w-full h-full border-0 bg-white"
      />
    );
  }

  if (kind === "csv") {
    return <CsvTable text={content} />;
  }

  return (
    <pre className="p-4 text-[13px] leading-relaxed font-mono whitespace-pre-wrap break-words text-foreground">
      {content}
    </pre>
  );
}
