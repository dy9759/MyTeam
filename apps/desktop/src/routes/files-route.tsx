import { useEffect, useState } from "react";
import type { FileIndex } from "@myteam/client-core";
import { RouteShell } from "@/components/route-shell";
import { desktopApi, useDesktopWorkspaceStore } from "@/lib/desktop-client";

export function FilesRoute() {
  const workspace = useDesktopWorkspaceStore((state) => state.workspace);
  const [files, setFiles] = useState<FileIndex[]>([]);

  useEffect(() => {
    if (!workspace?.id) return;
    void desktopApi.listFiles().then(setFiles);
  }, [workspace?.id]);

  return (
    <RouteShell
      eyebrow="Files"
      title="Workspace assets and references"
      description="Files is the desktop-first asset center. It keeps the index from the backend and adds native open/reveal actions when paths are available locally."
    >
      <div className="grid gap-4">
        {files.length === 0 ? (
          <div className="rounded-[28px] border border-dashed border-border/70 bg-card/70 px-6 py-16 text-center text-sm text-muted-foreground">
            No files indexed in this workspace yet.
          </div>
        ) : (
          files.map((file) => {
            const localPath = file.storage_path && file.storage_path.startsWith("/") ? file.storage_path : null;
            return (
              <article
                key={file.id}
                className="grid gap-4 rounded-[28px] border border-border/70 bg-card/85 p-5 md:grid-cols-[1.1fr_0.9fr]"
              >
                <div>
                  <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground">
                    {file.source_type}
                  </p>
                  <h3 className="mt-2 text-lg font-medium text-foreground">{file.file_name}</h3>
                  <p className="mt-2 text-sm text-muted-foreground">
                    {file.content_type ?? "Unknown type"} · {file.file_size ?? 0} bytes
                  </p>
                </div>
                <div className="flex flex-wrap items-center gap-3 justify-start md:justify-end">
                  <button
                    type="button"
                    onClick={() => {
                      if (localPath) {
                        void window.myteam.files.openPath(localPath);
                      }
                    }}
                    disabled={!localPath}
                    className="rounded-2xl border border-border/70 px-4 py-2 text-sm text-foreground disabled:cursor-not-allowed disabled:opacity-40"
                  >
                    Open
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      if (localPath) {
                        void window.myteam.files.revealPath(localPath);
                      }
                    }}
                    disabled={!localPath}
                    className="rounded-2xl border border-border/70 px-4 py-2 text-sm text-foreground disabled:cursor-not-allowed disabled:opacity-40"
                  >
                    Reveal in Finder
                  </button>
                </div>
              </article>
            );
          })
        )}
      </div>
    </RouteShell>
  );
}
