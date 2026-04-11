"use client";

import { useEffect, useState } from "react";
import { api } from "@/shared/api";
import type { WorkspaceMetrics } from "@/shared/types";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import {
  CheckCircle2Icon,
  ClockIcon,
  AlertTriangleIcon,
  FolderOpenIcon,
  PlayCircleIcon,
  BellRingIcon,
} from "lucide-react";

function formatDuration(seconds: number): string {
  if (seconds < 60) return `${Math.round(seconds)}s`;
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`;
  const hours = Math.floor(seconds / 3600);
  const mins = Math.round((seconds % 3600) / 60);
  return mins > 0 ? `${hours}h ${mins}m` : `${hours}h`;
}

function formatPercent(rate: number): string {
  return `${Math.round(rate * 100)}%`;
}

interface MetricCardProps {
  title: string;
  value: string;
  icon: React.ElementType;
}

function MetricCard({ title, value, icon: Icon }: MetricCardProps) {
  return (
    <Card size="sm">
      <CardHeader>
        <div className="flex items-center gap-2">
          <Icon className="size-4 text-muted-foreground" />
          <CardTitle className="text-xs font-medium text-muted-foreground">
            {title}
          </CardTitle>
        </div>
      </CardHeader>
      <CardContent>
        <div className="text-2xl font-bold">{value}</div>
      </CardContent>
    </Card>
  );
}

export function MetricsOverview() {
  const [metrics, setMetrics] = useState<WorkspaceMetrics | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);

  useEffect(() => {
    let cancelled = false;
    api
      .getWorkspaceMetrics()
      .then((data) => {
        if (!cancelled) setMetrics(data);
      })
      .catch(() => {
        if (!cancelled) setError(true);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  if (error) return null;

  if (loading) {
    return (
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
        {Array.from({ length: 6 }).map((_, i) => (
          <Card key={i} size="sm">
            <CardHeader>
              <Skeleton className="h-4 w-24" />
            </CardHeader>
            <CardContent>
              <Skeleton className="h-8 w-16" />
            </CardContent>
          </Card>
        ))}
      </div>
    );
  }

  if (!metrics) return null;

  const cards: MetricCardProps[] = [
    {
      title: "Completion Rate",
      value: formatPercent(metrics.task_completion_rate),
      icon: CheckCircle2Icon,
    },
    {
      title: "Avg Duration",
      value: formatDuration(metrics.average_task_duration_seconds),
      icon: ClockIcon,
    },
    {
      title: "Timeout Rate",
      value: formatPercent(metrics.timeout_rate),
      icon: AlertTriangleIcon,
    },
    {
      title: "Active Projects",
      value: String(metrics.active_projects),
      icon: FolderOpenIcon,
    },
    {
      title: "Active Runs",
      value: String(metrics.active_runs),
      icon: PlayCircleIcon,
    },
    {
      title: "Pending Escalations",
      value: String(metrics.pending_escalations),
      icon: BellRingIcon,
    },
  ];

  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
      {cards.map((card) => (
        <MetricCard key={card.title} {...card} />
      ))}
    </div>
  );
}
