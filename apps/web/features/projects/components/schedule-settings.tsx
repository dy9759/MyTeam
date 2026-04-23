"use client";

import { useState } from "react";
import type { Project, ProjectScheduleType } from "@/shared/types/project";

interface ScheduleSettingsProps {
  project: Project;
  onUpdate: (data: Partial<Project>) => void;
}

const CRON_PRESETS = [
  { label: "Every hour", value: "0 * * * *" },
  { label: "Daily at 9am", value: "0 9 * * *" },
  { label: "Weekly (Mon 9am)", value: "0 9 * * 1" },
  { label: "Monthly (1st, 9am)", value: "0 9 1 * *" },
];

export function ScheduleSettings({ project, onUpdate }: ScheduleSettingsProps) {
  const [scheduleType, setScheduleType] = useState<ProjectScheduleType>(project.schedule_type);
  const [cronExpr, setCronExpr] = useState(project.cron_expr ?? "");

  const handleSave = () => {
    onUpdate({ schedule_type: scheduleType, cron_expr: cronExpr || undefined });
  };

  return (
    <div className="space-y-4">
      <div>
        <label className="text-sm font-medium text-foreground">Schedule Type</label>
        <div className="mt-2 flex gap-2">
          {(["one_time", "scheduled_once", "recurring"] as const).map((t) => (
            <button
              key={t}
              type="button"
              onClick={() => setScheduleType(t)}
              className={`rounded-lg px-3 py-1.5 text-sm transition ${
                scheduleType === t
                  ? "bg-primary text-primary-foreground"
                  : "bg-secondary text-secondary-foreground hover:bg-secondary/80"
              }`}
            >
              {t === "one_time" ? "One-time" : t === "scheduled_once" ? "Scheduled" : "Recurring"}
            </button>
          ))}
        </div>
      </div>

      {(scheduleType === "scheduled_once" || scheduleType === "recurring") && (
        <div>
          <label className="text-sm font-medium text-foreground">
            {scheduleType === "scheduled_once" ? "Run at" : "Cron Expression"}
          </label>
          {scheduleType === "recurring" && (
            <div className="mt-2 flex flex-wrap gap-1">
              {CRON_PRESETS.map((preset) => (
                <button
                  key={preset.value}
                  type="button"
                  onClick={() => setCronExpr(preset.value)}
                  className={`rounded-md px-2 py-1 text-xs transition ${
                    cronExpr === preset.value
                      ? "bg-primary/20 text-primary"
                      : "bg-secondary text-muted-foreground hover:text-foreground"
                  }`}
                >
                  {preset.label}
                </button>
              ))}
            </div>
          )}
          <input
            type={scheduleType === "scheduled_once" ? "datetime-local" : "text"}
            value={cronExpr}
            onChange={(e) => setCronExpr(e.target.value)}
            placeholder={scheduleType === "recurring" ? "0 9 * * 1-5" : ""}
            className="mt-2 w-full rounded-lg border border-border bg-background px-3 py-2 text-sm"
          />
        </div>
      )}

      <button
        type="button"
        onClick={handleSave}
        className="rounded-lg bg-primary px-4 py-2 text-sm font-medium text-primary-foreground"
      >
        Save Schedule
      </button>
    </div>
  );
}
