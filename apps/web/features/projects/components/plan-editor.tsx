"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import { useWorkspaceStore } from "@/features/workspace";
import { Plus, Trash2, ChevronUp, ChevronDown } from "lucide-react";
import type { PlanStep } from "@/shared/types";

interface PlanEditorStep extends PlanStep {
  agent_id?: string;
}

interface PlanEditorProps {
  steps: PlanEditorStep[];
  onUpdate: (steps: PlanEditorStep[]) => void;
  readOnly?: boolean;
}

export function PlanEditor({ steps, onUpdate, readOnly }: PlanEditorProps) {
  const agents = useWorkspaceStore((s) => s.agents).filter((a) => !a.archived_at);
  const [editingIndex, setEditingIndex] = useState<number | null>(null);
  const [skillInput, setSkillInput] = useState("");

  function updateStep(index: number, updates: Partial<PlanEditorStep>) {
    const updated = steps.map((s, i) => (i === index ? { ...s, ...updates } : s));
    onUpdate(updated);
  }

  function addStep() {
    const newStep: PlanEditorStep = {
      order: steps.length + 1,
      description: "",
      required_skills: [],
      estimated_minutes: 30,
      depends_on: [],
      parallelizable: false,
    };
    onUpdate([...steps, newStep]);
    setEditingIndex(steps.length);
  }

  function removeStep(index: number) {
    const updated = steps
      .filter((_, i) => i !== index)
      .map((s, i) => ({ ...s, order: i + 1 }));
    onUpdate(updated);
    if (editingIndex === index) setEditingIndex(null);
  }

  function moveStep(index: number, direction: "up" | "down") {
    if (direction === "up" && index === 0) return;
    if (direction === "down" && index === steps.length - 1) return;
    const swapIndex = direction === "up" ? index - 1 : index + 1;
    const updated = [...steps];
    const a = updated[index];
    const b = updated[swapIndex];
    if (!a || !b) return;
    [updated[index], updated[swapIndex]] = [b, a];
    onUpdate(updated.map((s, i) => ({ ...s, order: i + 1 })));
    setEditingIndex(swapIndex);
  }

  function addSkill(index: number) {
    if (!skillInput.trim()) return;
    const step = steps[index];
    if (!step) return;
    if (!step.required_skills.includes(skillInput.trim())) {
      updateStep(index, { required_skills: [...step.required_skills, skillInput.trim()] });
    }
    setSkillInput("");
  }

  function removeSkill(index: number, skill: string) {
    const step = steps[index];
    if (!step) return;
    updateStep(index, { required_skills: step.required_skills.filter((s) => s !== skill) });
  }

  return (
    <div className="space-y-3">
      {steps.map((step, index) => {
        const isEditing = editingIndex === index;

        return (
          <div
            key={index}
            className={`border rounded-lg p-4 transition-all ${isEditing ? "ring-2 ring-primary ring-offset-2" : ""} ${!readOnly ? "cursor-pointer hover:bg-accent/30" : ""}`}
            onClick={() => !readOnly && setEditingIndex(isEditing ? null : index)}
          >
            <div className="flex items-start gap-3">
              {/* Step number */}
              <div className="w-7 h-7 rounded-full bg-muted flex items-center justify-center text-sm font-bold shrink-0">
                {step.order}
              </div>

              <div className="flex-1 min-w-0">
                {isEditing && !readOnly ? (
                  <div className="space-y-3" onClick={(e) => e.stopPropagation()}>
                    {/* Description */}
                    <div>
                      <label className="text-xs text-muted-foreground">描述</label>
                      <Textarea
                        value={step.description}
                        onChange={(e) => updateStep(index, { description: e.target.value })}
                        rows={2}
                        className="mt-1"
                      />
                    </div>

                    {/* Agent selector */}
                    <div>
                      <label className="text-xs text-muted-foreground">分配 Agent</label>
                      <select
                        value={step.agent_id ?? ""}
                        onChange={(e) => updateStep(index, { agent_id: e.target.value || undefined })}
                        className="w-full mt-1 px-3 py-1.5 text-sm border rounded-md bg-background"
                      >
                        <option value="">未分配</option>
                        {agents.map((a) => (
                          <option key={a.id} value={a.id}>{a.name}</option>
                        ))}
                      </select>
                    </div>

                    {/* Skills */}
                    <div>
                      <label className="text-xs text-muted-foreground">所需技能</label>
                      <div className="flex flex-wrap gap-1 mt-1">
                        {step.required_skills.map((skill) => (
                          <Badge key={skill} variant="secondary" className="gap-1">
                            {skill}
                            <button onClick={() => removeSkill(index, skill)} className="hover:text-destructive" aria-label={`移除技能 ${skill}`}>
                              <span className="text-xs">&times;</span>
                            </button>
                          </Badge>
                        ))}
                      </div>
                      <div className="flex gap-2 mt-1">
                        <Input
                          value={skillInput}
                          onChange={(e) => setSkillInput(e.target.value)}
                          onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); addSkill(index); } }}
                          placeholder="添加技能标签..."
                          className="text-sm"
                        />
                        <Button variant="outline" size="sm" onClick={() => addSkill(index)}>
                          添加
                        </Button>
                      </div>
                    </div>

                    {/* Estimated time & dependencies */}
                    <div className="flex gap-4">
                      <div className="flex-1">
                        <label className="text-xs text-muted-foreground">预估时间（分钟）</label>
                        <Input
                          type="number"
                          value={step.estimated_minutes}
                          onChange={(e) => updateStep(index, { estimated_minutes: parseInt(e.target.value) || 0 })}
                          className="mt-1"
                        />
                      </div>
                      <div className="flex-1">
                        <label className="text-xs text-muted-foreground">依赖步骤（逗号分隔）</label>
                        <Input
                          value={step.depends_on.join(", ")}
                          onChange={(e) => {
                            const deps = e.target.value
                              .split(",")
                              .map((s) => parseInt(s.trim()))
                              .filter((n) => !isNaN(n));
                            updateStep(index, { depends_on: deps });
                          }}
                          placeholder="例如：1, 2"
                          className="mt-1"
                        />
                      </div>
                    </div>
                  </div>
                ) : (
                  <>
                    <div className="font-medium text-sm">
                      {step.description || "（未设置描述）"}
                    </div>
                    <div className="flex items-center gap-2 mt-1 flex-wrap">
                      {step.agent_id && (
                        <span className="text-xs text-muted-foreground">
                          Agent: {agents.find((a) => a.id === step.agent_id)?.name ?? step.agent_id.slice(0, 8)}
                        </span>
                      )}
                      {step.estimated_minutes > 0 && (
                        <span className="text-xs text-muted-foreground">
                          ~{step.estimated_minutes}分钟
                        </span>
                      )}
                      {step.depends_on.length > 0 && (
                        <span className="text-xs text-muted-foreground">
                          依赖: {step.depends_on.join(", ")}
                        </span>
                      )}
                    </div>
                    {step.required_skills.length > 0 && (
                      <div className="flex gap-1 mt-1.5 flex-wrap">
                        {step.required_skills.map((skill) => (
                          <span key={skill} className="text-xs bg-primary/10 text-primary px-1.5 py-0.5 rounded">
                            {skill}
                          </span>
                        ))}
                      </div>
                    )}
                  </>
                )}
              </div>

              {/* Actions */}
              {!readOnly && (
                <div className="flex flex-col gap-1 shrink-0" onClick={(e) => e.stopPropagation()}>
                  <button onClick={() => moveStep(index, "up")} disabled={index === 0}
                    className="p-1 hover:bg-accent rounded disabled:opacity-30"
                    aria-label="上移步骤">
                    <ChevronUp className="size-3.5" />
                  </button>
                  <button onClick={() => moveStep(index, "down")} disabled={index === steps.length - 1}
                    className="p-1 hover:bg-accent rounded disabled:opacity-30"
                    aria-label="下移步骤">
                    <ChevronDown className="size-3.5" />
                  </button>
                  <button onClick={() => removeStep(index)}
                    className="p-1 hover:bg-accent rounded text-muted-foreground hover:text-destructive"
                    aria-label="删除步骤">
                    <Trash2 className="size-3.5" />
                  </button>
                </div>
              )}
            </div>
          </div>
        );
      })}

      {steps.length === 0 && (
        <div className="text-center py-8 border-2 border-dashed rounded-lg text-muted-foreground">
          <p className="mb-2">暂无计划步骤</p>
          {!readOnly && (
            <Button variant="outline" size="sm" onClick={addStep}>
              <Plus className="size-3.5 mr-1" />
              添加第一个步骤
            </Button>
          )}
        </div>
      )}

      {!readOnly && steps.length > 0 && (
        <Button variant="outline" size="sm" onClick={addStep} className="w-full">
          <Plus className="size-3.5 mr-1" />
          添加步骤
        </Button>
      )}
    </div>
  );
}
