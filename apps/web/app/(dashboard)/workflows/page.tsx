"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { useWorkflowStore } from "@/features/workflow";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { api } from "@/shared/api";
import { toast } from "sonner";
import { Plus, Sparkles, Loader2 } from "lucide-react";
import type { Workflow } from "@/shared/types/workflow";

const statusColor: Record<string, string> = {
  draft: "bg-muted text-muted-foreground",
  pending: "bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200",
  running: "bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200",
  completed: "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200",
  failed: "bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200",
  cancelled: "bg-gray-100 text-gray-800 dark:bg-gray-900 dark:text-gray-200",
};

export default function WorkflowsPage() {
  const router = useRouter();
  const { workflows, loading, fetchWorkflows } = useWorkflowStore();
  const [createOpen, setCreateOpen] = useState(false);
  const [generateOpen, setGenerateOpen] = useState(false);
  const [title, setTitle] = useState("");
  const [creating, setCreating] = useState(false);
  const [generateInput, setGenerateInput] = useState("");
  const [generating, setGenerating] = useState(false);

  useEffect(() => {
    fetchWorkflows();
  }, [fetchWorkflows]);

  const handleCreate = async () => {
    if (!title.trim()) return;
    setCreating(true);
    try {
      const wf = await api.createWorkflow({ title: title.trim() });
      toast.success("Workflow created");
      setCreateOpen(false);
      setTitle("");
      fetchWorkflows();
      router.push(`/workflows/${wf.id}`);
    } catch {
      toast.error("Failed to create workflow");
    } finally {
      setCreating(false);
    }
  };

  const handleGenerate = async () => {
    if (!generateInput.trim()) return;
    setGenerating(true);
    try {
      const plan = await api.generatePlan(generateInput.trim());
      toast.success("Plan generated, creating workflow...");
      const wf = await api.createWorkflow({ title: plan.title, plan_id: plan.id });
      toast.success("Workflow created from plan");
      setGenerateOpen(false);
      setGenerateInput("");
      fetchWorkflows();
      router.push(`/workflows/${wf.id}`);
    } catch {
      toast.error("Failed to generate plan");
    } finally {
      setGenerating(false);
    }
  };

  return (
    <div className="flex-1 overflow-auto p-6">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-semibold">Workflows</h1>
        <div className="flex gap-2">
          <Button variant="outline" onClick={() => setGenerateOpen(true)}>
            <Sparkles className="size-4 mr-2" />
            Generate from Chat
          </Button>
          <Button onClick={() => setCreateOpen(true)}>
            <Plus className="size-4 mr-2" />
            New Workflow
          </Button>
        </div>
      </div>

      {loading ? (
        <div className="flex items-center justify-center py-20">
          <Loader2 className="size-6 animate-spin text-muted-foreground" />
        </div>
      ) : workflows.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-20 text-muted-foreground">
          <p className="text-lg mb-2">No workflows yet</p>
          <p className="text-sm">Create a workflow or generate one from a chat prompt.</p>
        </div>
      ) : (
        <div className="space-y-2">
          {workflows.map((wf: Workflow) => (
            <div
              key={wf.id}
              className="flex items-center gap-4 p-4 border rounded-lg cursor-pointer hover:bg-accent/50 transition-colors"
              onClick={() => router.push(`/workflows/${wf.id}`)}
            >
              <div className="flex-1 min-w-0">
                <div className="font-medium truncate">{wf.title}</div>
                <div className="text-sm text-muted-foreground">
                  {wf.type} · v{wf.version} · {new Date(wf.created_at).toLocaleDateString()}
                </div>
              </div>
              <Badge className={statusColor[wf.status] ?? ""} variant="outline">
                {wf.status}
              </Badge>
            </div>
          ))}
        </div>
      )}

      {/* Create Dialog */}
      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>New Workflow</DialogTitle>
          </DialogHeader>
          <Input
            placeholder="Workflow title"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && handleCreate()}
          />
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreateOpen(false)}>Cancel</Button>
            <Button onClick={handleCreate} disabled={creating || !title.trim()}>
              {creating && <Loader2 className="size-4 mr-2 animate-spin" />}
              Create
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Generate Dialog */}
      <Dialog open={generateOpen} onOpenChange={setGenerateOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Generate from Chat</DialogTitle>
          </DialogHeader>
          <Textarea
            placeholder="Describe what you want to accomplish..."
            value={generateInput}
            onChange={(e) => setGenerateInput(e.target.value)}
            rows={4}
          />
          <DialogFooter>
            <Button variant="outline" onClick={() => setGenerateOpen(false)}>Cancel</Button>
            <Button onClick={handleGenerate} disabled={generating || !generateInput.trim()}>
              {generating && <Loader2 className="size-4 mr-2 animate-spin" />}
              Generate
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
