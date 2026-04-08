"use client"
import { useState } from "react"
import type { WorkflowStep } from "@/shared/types/workflow"

interface WorkflowEditorProps {
  steps: WorkflowStep[]
  onReorder?: (steps: WorkflowStep[]) => void
  onUpdateStep?: (stepId: string, updates: Partial<WorkflowStep>) => void
  onAddStep?: () => void
  onRemoveStep?: (stepId: string) => void
  readOnly?: boolean
}

const STATUS_COLORS: Record<string, string> = {
  pending: "border-gray-300 bg-gray-50",
  running: "border-yellow-400 bg-yellow-50 animate-pulse",
  completed: "border-green-400 bg-green-50",
  failed: "border-red-400 bg-red-50",
}

export function WorkflowEditor({ steps, onUpdateStep, onAddStep, onRemoveStep, readOnly }: WorkflowEditorProps) {
  const [selectedStep, setSelectedStep] = useState<string | null>(null)
  const [_draggedStep, setDraggedStep] = useState<string | null>(null)

  // Sort steps by order
  const sortedSteps = [...steps].sort((a, b) => a.step_order - b.step_order)

  // Build dependency graph for connector lines
  function getStepDeps(step: WorkflowStep): string[] {
    return step.depends_on ?? []
  }

  return (
    <div className="relative">
      {/* Toolbar */}
      {!readOnly && (
        <div className="flex gap-2 mb-4 p-3 border rounded-lg bg-muted/30">
          <button onClick={onAddStep}
            className="px-3 py-1.5 text-sm bg-primary text-primary-foreground rounded-md">
            + 添加步骤
          </button>
          <span className="text-sm text-muted-foreground self-center">
            拖拽步骤重新排序 · 点击编辑
          </span>
        </div>
      )}

      {/* Visual DAG */}
      <div className="space-y-3">
        {sortedSteps.map((step, index) => {
          const deps = getStepDeps(step)
          const isSelected = selectedStep === step.id

          return (
            <div key={step.id}>
              {/* Connector line */}
              {index > 0 && (
                <div className="flex justify-center py-1">
                  <div className="w-0.5 h-6 bg-border" />
                  <span className="absolute text-xs text-muted-foreground -mt-1 ml-4">
                    {deps.length > 0 ? `依赖步骤 ${deps.join(", ")}` : "顺序执行"}
                  </span>
                </div>
              )}

              {/* Step node */}
              <div
                className={`relative border-2 rounded-xl p-4 cursor-pointer transition-all ${STATUS_COLORS[step.status] ?? STATUS_COLORS.pending} ${isSelected ? "ring-2 ring-primary ring-offset-2" : ""} ${!readOnly ? "hover:shadow-md" : ""}`}
                onClick={() => setSelectedStep(isSelected ? null : step.id)}
                draggable={!readOnly}
                onDragStart={() => setDraggedStep(step.id)}
                onDragEnd={() => setDraggedStep(null)}
              >
                <div className="flex items-start justify-between">
                  <div className="flex items-center gap-3">
                    {/* Step number */}
                    <div className={`w-8 h-8 rounded-full flex items-center justify-center text-sm font-bold ${step.status === "completed" ? "bg-green-500 text-white" : step.status === "running" ? "bg-yellow-500 text-white" : step.status === "failed" ? "bg-red-500 text-white" : "bg-muted text-muted-foreground"}`}>
                      {step.status === "completed" ? "\u2713" : step.status === "failed" ? "\u2715" : step.step_order}
                    </div>

                    <div>
                      <div className="font-medium">{step.description}</div>
                      <div className="flex gap-2 mt-1 text-xs text-muted-foreground">
                        {step.agent_id && <span>{step.agent_id.slice(0, 8)}</span>}
                        {step.timeout_ms && <span>{step.timeout_ms / 1000}秒超时</span>}
                        {step.retry_count > 1 && <span>{step.retry_count}次重试</span>}
                      </div>
                    </div>
                  </div>

                  <div className="flex items-center gap-2">
                    <span className={`text-xs px-2 py-0.5 rounded-full ${step.status === "completed" ? "bg-green-100 text-green-700" : step.status === "running" ? "bg-yellow-100 text-yellow-700" : step.status === "failed" ? "bg-red-100 text-red-700" : "bg-gray-100 text-gray-600"}`}>
                      {step.status}
                    </span>
                    {!readOnly && onRemoveStep && (
                      <button onClick={(e) => { e.stopPropagation(); onRemoveStep(step.id) }}
                        className="text-muted-foreground hover:text-destructive text-sm">{"\u2715"}</button>
                    )}
                  </div>
                </div>

                {/* Skills */}
                {step.required_skills?.length > 0 && (
                  <div className="flex gap-1 mt-2">
                    {step.required_skills.map(s => (
                      <span key={s} className="text-xs bg-primary/10 text-primary px-1.5 py-0.5 rounded">{s}</span>
                    ))}
                  </div>
                )}

                {/* Expanded edit panel */}
                {isSelected && !readOnly && (
                  <div className="mt-3 pt-3 border-t space-y-2" onClick={e => e.stopPropagation()}>
                    <div>
                      <label className="text-xs text-muted-foreground">描述</label>
                      <input value={step.description}
                        onChange={e => onUpdateStep?.(step.id, { description: e.target.value })}
                        className="w-full mt-1 px-2 py-1 text-sm border rounded bg-background" />
                    </div>
                    <div className="flex gap-2">
                      <div className="flex-1">
                        <label className="text-xs text-muted-foreground">超时（毫秒）</label>
                        <input type="number" value={step.timeout_ms}
                          onChange={e => onUpdateStep?.(step.id, { timeout_ms: parseInt(e.target.value) })}
                          className="w-full mt-1 px-2 py-1 text-sm border rounded bg-background" />
                      </div>
                      <div className="flex-1">
                        <label className="text-xs text-muted-foreground">重试</label>
                        <input type="number" value={step.retry_count}
                          onChange={e => onUpdateStep?.(step.id, { retry_count: parseInt(e.target.value) })}
                          className="w-full mt-1 px-2 py-1 text-sm border rounded bg-background" />
                      </div>
                    </div>
                  </div>
                )}

                {/* Result/error */}
                {step.result && (
                  <div className="mt-2 text-xs bg-green-50 text-green-700 p-2 rounded">
                    结果：{JSON.stringify(step.result).slice(0, 100)}
                  </div>
                )}
                {step.error && (
                  <div className="mt-2 text-xs bg-red-50 text-red-700 p-2 rounded">
                    错误：{step.error}
                  </div>
                )}
              </div>
            </div>
          )
        })}
      </div>

      {sortedSteps.length === 0 && (
        <div className="text-center py-12 border-2 border-dashed rounded-xl text-muted-foreground">
          <p className="mb-2">暂无步骤</p>
          {!readOnly && <button onClick={onAddStep} className="text-sm text-primary hover:underline">添加第一个步骤</button>}
        </div>
      )}
    </div>
  )
}
