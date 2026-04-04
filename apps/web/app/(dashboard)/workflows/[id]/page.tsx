"use client";

import { useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { api } from "@/shared/api";
import { toast } from "sonner";
import { ArrowLeft, Play, Loader2, Trash2 } from "lucide-react";
import type { Workflow, WorkflowStep } from "@/shared/types/workflow";

const statusColor = (status: string) => {
  switch (status) {
    case "running": return "bg-blue-500";
    case "completed": return "bg-green-500";
    case "failed": return "bg-red-500";
    case "pending": return "bg-yellow-500";
    default: return "bg-gray-400";
  }
};

const statusBadge = (status: string) => {
  switch (status) {
    case "running": return "bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200";
    case "completed": return "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200";
    case "failed": return "bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200";
    case "pending": return "bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200";
    default: return "bg-gray-100 text-gray-800 dark:bg-gray-900 dark:text-gray-200";
  }
};

const workflowStatusBadge: Record<string, string> = {
  draft: "bg-muted text-muted-foreground",
  pending: "bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200",
  running: "bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200",
  completed: "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200",
  failed: "bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200",
  cancelled: "bg-gray-100 text-gray-800 dark:bg-gray-900 dark:text-gray-200",
};

export default function WorkflowDetailPage() {
  const params = useParams();
  const router = useRouter();
  const id = params.id as string;
  const [workflow, setWorkflow] = useState<Workflow | null>(null);
  const [steps, setSteps] = useState<WorkflowStep[]>([]);
  const [loading, setLoading] = useState(true);
  const [starting, setStarting] = useState(false);

  useEffect(() => {
    if (!id) return;
    const load = async () => {
      try {
        const [wf, stepsRes] = await Promise.all([
          api.getWorkflow(id),
          api.getWorkflowSteps(id),
        ]);
        setWorkflow(wf as Workflow);
        setSteps((stepsRes.steps ?? []) as WorkflowStep[]);
      } catch {
        toast.error("Failed to load workflow");
      } finally {
        setLoading(false);
      }
    };
    load();
  }, [id]);

  const handleStart = async () => {
    setStarting(true);
    try {
      const updated = await api.startWorkflow(id);
      setWorkflow(updated as Workflow);
      toast.success("Workflow started");
    } catch {
      toast.error("Failed to start workflow");
    } finally {
      setStarting(false);
    }
  };

  const handleDelete = async () => {
    try {
      await api.deleteWorkflow(id);
      toast.success("Workflow deleted");
      router.push("/workflows");
    } catch {
      toast.error("Failed to delete workflow");
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center flex-1 py-20">
        <Loader2 className="size-6 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (!workflow) {
    return (
      <div className="flex-1 p-6">
        <p className="text-muted-foreground">Workflow not found.</p>
      </div>
    );
  }

  const canStart = workflow.status === "draft" || workflow.status === "pending";

  return (
    <div className="flex-1 overflow-auto p-6">
      {/* Header */}
      <div className="flex items-center gap-3 mb-6">
        <Button variant="ghost" size="icon" onClick={() => router.push("/workflows")}>
          <ArrowLeft className="size-4" />
        </Button>
        <div className="flex-1 min-w-0">
          <h1 className="text-2xl font-semibold truncate">{workflow.title}</h1>
          <div className="flex items-center gap-2 mt-1 text-sm text-muted-foreground">
            <Badge className={workflowStatusBadge[workflow.status] ?? ""} variant="outline">
              {workflow.status}
            </Badge>
            <span>Type: {workflow.type}</span>
            <span>v{workflow.version}</span>
            {workflow.cron_expr && <span>Cron: {workflow.cron_expr}</span>}
          </div>
        </div>
        <div className="flex gap-2">
          {canStart && (
            <Button onClick={handleStart} disabled={starting}>
              {starting ? <Loader2 className="size-4 mr-2 animate-spin" /> : <Play className="size-4 mr-2" />}
              Start
            </Button>
          )}
          <Button variant="outline" size="icon" onClick={handleDelete}>
            <Trash2 className="size-4" />
          </Button>
        </div>
      </div>

      {/* Steps */}
      <div className="space-y-2">
        <h2 className="text-lg font-medium mb-3">Steps</h2>
        {steps.length === 0 ? (
          <p className="text-muted-foreground text-sm">No steps defined for this workflow.</p>
        ) : (
          steps.map((step) => (
            <div key={step.id} className="flex items-center gap-3 p-4 border rounded-lg">
              <div className={`w-3 h-3 rounded-full ${statusColor(step.status)}`} />
              <div className="flex-1">
                <div className="font-medium">Step {step.step_order}: {step.description}</div>
                <div className="text-sm text-muted-foreground">
                  Agent: {step.agent_id || "unassigned"} · Timeout: {step.timeout_ms / 1000}s
                  {step.required_skills.length > 0 && ` · Skills: ${step.required_skills.join(", ")}`}
                  {step.depends_on.length > 0 && ` · Depends on: ${step.depends_on.join(", ")}`}
                </div>
                {step.error && (
                  <div className="text-sm text-red-500 mt-1">Error: {step.error}</div>
                )}
              </div>
              <span className={`text-xs px-2 py-0.5 rounded ${statusBadge(step.status)}`}>
                {step.status}
              </span>
            </div>
          ))
        )}
      </div>
    </div>
  );
}
