import { useEffect, useState } from "react";
import type { DesktopShellConfig } from "../../electron/preload-api";
import { RouteShell } from "@/components/route-shell";

export function SettingsRoute() {
  const [config, setConfig] = useState<DesktopShellConfig | null>(null);
  const [daemonAction, setDaemonAction] = useState<"idle" | "starting" | "stopping">("idle");

  useEffect(() => {
    void window.myteam.shell.getConfig().then(setConfig);
  }, []);

  return (
    <RouteShell
      eyebrow="Runtime"
      title="Desktop configuration and daemon control"
      description="Settings is now where desktop defaults, runtime integration, and daemon lifecycle live. Identity stays in Account."
      actions={
        <div className="flex items-center gap-3">
          <button
            type="button"
            onClick={() => {
              setDaemonAction("starting");
              void window.myteam.runtime.startDaemon().finally(() => setDaemonAction("idle"));
            }}
            className="rounded-2xl bg-primary px-4 py-2 text-sm font-medium text-primary-foreground"
          >
            {daemonAction === "starting" ? "Starting…" : "Start daemon"}
          </button>
          <button
            type="button"
            onClick={() => {
              setDaemonAction("stopping");
              void window.myteam.runtime.stopDaemon().finally(() => setDaemonAction("idle"));
            }}
            className="rounded-2xl border border-border/70 px-4 py-2 text-sm text-foreground"
          >
            {daemonAction === "stopping" ? "Stopping…" : "Stop daemon"}
          </button>
        </div>
      }
    >
      <div className="grid gap-6 lg:grid-cols-2">
        <section className="rounded-[28px] border border-border/70 bg-card/85 p-6">
          <p className="text-xs uppercase tracking-[0.22em] text-muted-foreground">
            App endpoints
          </p>
          <dl className="mt-4 space-y-3 text-sm">
            <ConfigRow label="App URL" value={config?.appUrl ?? "Loading…"} />
            <ConfigRow label="API URL" value={config?.apiBaseUrl ?? "Loading…"} />
            <ConfigRow label="WS URL" value={config?.wsUrl ?? "Loading…"} />
            <ConfigRow label="CLI path" value={config?.cliPath ?? "Loading…"} mono />
          </dl>
        </section>
        <section className="rounded-[28px] border border-border/70 bg-card/85 p-6">
          <p className="text-xs uppercase tracking-[0.22em] text-muted-foreground">
            Desktop defaults
          </p>
          <div className="mt-4 grid gap-4">
            <SettingCard
              title="Secure auth"
              description="PAT is stored in the macOS keychain through the Swift helper instead of browser storage."
            />
            <SettingCard
              title="Workspace persistence"
              description="Workspace selection is stored in electron-store through preload, not in Next cookies."
            />
            <SettingCard
              title="Daemon linkage"
              description="Electron main owns `myteam daemon start/stop` and workspace watch commands."
            />
          </div>
        </section>
      </div>
    </RouteShell>
  );
}

function ConfigRow({
  label,
  value,
  mono = false,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <div className="rounded-2xl border border-border/70 bg-background/70 px-4 py-3">
      <dt className="text-xs uppercase tracking-[0.18em] text-muted-foreground">{label}</dt>
      <dd className={`mt-2 text-sm text-foreground ${mono ? "font-mono" : ""}`}>{value}</dd>
    </div>
  );
}

function SettingCard({
  title,
  description,
}: {
  title: string;
  description: string;
}) {
  return (
    <article className="rounded-2xl border border-border/70 bg-background/70 px-4 py-4">
      <h3 className="text-sm font-medium text-foreground">{title}</h3>
      <p className="mt-2 text-sm leading-6 text-muted-foreground">{description}</p>
    </article>
  );
}
