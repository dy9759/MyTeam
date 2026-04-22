"use client";

import { useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Label } from "@/components/ui/label";
import { useWorkspaceStore } from "@/features/workspace";
import { useChannelStore } from "@/features/channels/store";
import { useProjectStore } from "@/features/projects";
import { toast } from "sonner";
import { Loader2, X as XIcon, Search } from "lucide-react";
import type { Project, ProjectScheduleType } from "@/shared/types";

interface CreateProjectDialogProps {
  onClose: () => void;
  onCreated: (project: Project) => void;
}

type Step = 1 | 2 | 3 | 4;

const STEP_TITLES: Record<Step, string> = {
  1: "选择上下文",
  2: "选择 Agent",
  3: "设置",
  4: "审核",
};

export function CreateProjectDialog({ onClose, onCreated }: CreateProjectDialogProps) {
  const [step, setStep] = useState<Step>(1);

  // Step 1: Context
  const channels = useChannelStore((s) => s.channels);
  const [selectedSources, setSelectedSources] = useState<{ type: 'channel' | 'dm' | 'thread'; id: string; name: string }[]>([]);
  const [sourceFilter, setSourceFilter] = useState("");

  // Step 2: Agents
  const agents = useWorkspaceStore((s) => s.agents);
  const [selectedAgentIds, setSelectedAgentIds] = useState<string[]>([]);

  // Step 3: Settings
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [scheduleType, setScheduleType] = useState<ProjectScheduleType>("one_time");
  const [cronExpr, setCronExpr] = useState("");

  // Step 4: Submit
  const [submitting, setSubmitting] = useState(false);
  const createFromChat = useProjectStore((s) => s.createFromChat);
  const createProject = useProjectStore((s) => s.createProject);

  const filteredChannels = channels.filter((c) =>
    c.name?.toLowerCase().includes(sourceFilter.toLowerCase()),
  );

  function toggleSource(channelId: string, channelName: string) {
    setSelectedSources((prev) => {
      const exists = prev.find((s) => s.id === channelId);
      if (exists) return prev.filter((s) => s.id !== channelId);
      return [...prev, { type: "channel" as const, id: channelId, name: channelName }];
    });
  }

  function toggleAgent(agentId: string) {
    setSelectedAgentIds((prev) =>
      prev.includes(agentId) ? prev.filter((id) => id !== agentId) : [...prev, agentId],
    );
  }

  async function handleCreate() {
    if (!title.trim()) return;
    setSubmitting(true);
    try {
      let project: Project;
      if (selectedSources.length > 0 && selectedAgentIds.length > 0) {
        project = await createFromChat({
          title: title.trim(),
          source_refs: selectedSources.map((s) => ({ type: s.type, id: s.id })),
          agent_ids: selectedAgentIds,
          schedule_type: scheduleType,
          cron_expr: scheduleType !== "one_time" ? cronExpr : undefined,
        });
      } else {
        project = await createProject({
          title: title.trim(),
          description: description.trim() || undefined,
          schedule_type: scheduleType,
        });
      }
      toast.success("项目已创建");
      onCreated(project);
    } catch {
      toast.error("创建项目失败");
    } finally {
      setSubmitting(false);
    }
  }

  const canNext = () => {
    switch (step) {
      case 1: return true; // sources are optional
      case 2: return true; // agents are optional
      case 3: return !!title.trim();
      default: return false;
    }
  };

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>创建项目 - {STEP_TITLES[step]}</DialogTitle>
        </DialogHeader>

        {/* Step indicator */}
        <div className="flex items-center gap-1 mb-4">
          {([1, 2, 3, 4] as Step[]).map((s) => (
            <div
              key={s}
              className={`flex-1 h-1 rounded-full ${s <= step ? "bg-primary" : "bg-muted"}`}
            />
          ))}
        </div>

        {/* Step 1: Context */}
        {step === 1 && (
          <div className="space-y-3">
            <div className="relative">
              <Search className="absolute left-2.5 top-2.5 size-4 text-muted-foreground" />
              <Input
                value={sourceFilter}
                onChange={(e) => setSourceFilter(e.target.value)}
                placeholder="搜索频道..."
                className="pl-9"
              />
            </div>

            {/* Selected tags */}
            {selectedSources.length > 0 && (
              <div className="flex flex-wrap gap-1">
                {selectedSources.map((s) => (
                  <Badge key={s.id} variant="secondary" className="gap-1">
                    #{s.name}
                    <button onClick={() => toggleSource(s.id, s.name)} className="hover:text-destructive" aria-label={`移除 #${s.name}`}>
                      <XIcon className="size-3" />
                    </button>
                  </Badge>
                ))}
              </div>
            )}

            <div className="max-h-48 overflow-y-auto space-y-1 border rounded-md p-2">
              {filteredChannels.length > 0 ? (
                filteredChannels.map((ch) => (
                  <label
                    key={ch.id}
                    className="flex items-center gap-2 p-2 rounded-md hover:bg-accent/50 cursor-pointer text-sm"
                  >
                    <Checkbox
                      checked={selectedSources.some((s) => s.id === ch.id)}
                      onCheckedChange={() => toggleSource(ch.id, ch.name ?? ch.id)}
                    />
                    <span>#{ch.name ?? ch.id.slice(0, 8)}</span>
                  </label>
                ))
              ) : (
                <div className="text-sm text-muted-foreground text-center py-4">
                  暂无可用频道
                </div>
              )}
            </div>
            <p className="text-xs text-muted-foreground">
              选择要作为项目上下文的频道（可选）
            </p>
          </div>
        )}

        {/* Step 2: Agents */}
        {step === 2 && (
          <div className="space-y-3">
            <div className="max-h-60 overflow-y-auto space-y-1 border rounded-md p-2">
              {agents.filter((a) => !a.archived_at).length > 0 ? (
                agents
                  .filter((a) => !a.archived_at)
                  .map((agent) => (
                    <label
                      key={agent.id}
                      className="flex items-center gap-3 p-2 rounded-md hover:bg-accent/50 cursor-pointer"
                    >
                      <Checkbox
                        checked={selectedAgentIds.includes(agent.id)}
                        onCheckedChange={() => toggleAgent(agent.id)}
                      />
                      <div className="flex-1 min-w-0">
                        <div className="text-sm font-medium truncate">{agent.name}</div>
                        {agent.identity_card?.capabilities && agent.identity_card.capabilities.length > 0 && (
                          <div className="flex gap-1 mt-0.5 flex-wrap">
                            {agent.identity_card.capabilities.slice(0, 3).map((cap) => (
                              <span key={cap} className="text-xs bg-primary/10 text-primary px-1.5 py-0.5 rounded">
                                {cap}
                              </span>
                            ))}
                          </div>
                        )}
                      </div>
                    </label>
                  ))
              ) : (
                <div className="text-sm text-muted-foreground text-center py-4">
                  暂无可用 Agent
                </div>
              )}
            </div>
            <p className="text-xs text-muted-foreground">
              选择参与项目的 Agent（可选）
            </p>
          </div>
        )}

        {/* Step 3: Settings */}
        {step === 3 && (
          <div className="space-y-4">
            <div>
              <Label>项目标题 *</Label>
              <Input
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                placeholder="输入项目标题"
                className="mt-1"
              />
            </div>
            <div>
              <Label>描述</Label>
              <Textarea
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="项目描述（可选）"
                rows={3}
                className="mt-1"
              />
            </div>
            <div>
              <Label>调度类型</Label>
              <RadioGroup
                value={scheduleType}
                onValueChange={(v) => setScheduleType(v as ProjectScheduleType)}
                className="mt-2 space-y-2"
              >
                <div className="flex items-center gap-2">
                  <RadioGroupItem value="one_time" id="one_time" />
                  <Label htmlFor="one_time" className="font-normal">一次性</Label>
                </div>
                <div className="flex items-center gap-2">
                  <RadioGroupItem value="scheduled" id="scheduled" />
                  <Label htmlFor="scheduled" className="font-normal">定时</Label>
                </div>
                <div className="flex items-center gap-2">
                  <RadioGroupItem value="recurring" id="recurring" />
                  <Label htmlFor="recurring" className="font-normal">周期性</Label>
                </div>
              </RadioGroup>
            </div>
            {scheduleType !== "one_time" && (
              <div>
                <Label>Cron 表达式</Label>
                <Input
                  value={cronExpr}
                  onChange={(e) => setCronExpr(e.target.value)}
                  placeholder="例如：0 9 * * 1-5"
                  className="mt-1 font-mono"
                />
              </div>
            )}
          </div>
        )}

        {/* Step 4: Review */}
        {step === 4 && (
          <div className="space-y-4">
            <div>
              <div className="text-sm text-muted-foreground">标题</div>
              <div className="font-medium">{title}</div>
            </div>
            {description && (
              <div>
                <div className="text-sm text-muted-foreground">描述</div>
                <div className="text-sm">{description}</div>
              </div>
            )}
            {selectedSources.length > 0 && (
              <div>
                <div className="text-sm text-muted-foreground">上下文来源</div>
                <div className="flex flex-wrap gap-1 mt-1">
                  {selectedSources.map((s) => (
                    <Badge key={s.id} variant="secondary">#{s.name}</Badge>
                  ))}
                </div>
              </div>
            )}
            {selectedAgentIds.length > 0 && (
              <div>
                <div className="text-sm text-muted-foreground">参与 Agent</div>
                <div className="flex flex-wrap gap-1 mt-1">
                  {selectedAgentIds.map((agentId) => {
                    const agent = agents.find((a) => a.id === agentId);
                    return (
                      <Badge key={agentId} variant="secondary">
                        {agent?.name ?? agentId.slice(0, 8)}
                      </Badge>
                    );
                  })}
                </div>
              </div>
            )}
            <div>
              <div className="text-sm text-muted-foreground">调度类型</div>
              <div className="text-sm">
                {scheduleType === "one_time" ? "一次性" : scheduleType === "scheduled" ? "定时" : "周期性"}
                {cronExpr && ` (${cronExpr})`}
              </div>
            </div>
          </div>
        )}

        <DialogFooter className="gap-2">
          {step > 1 && (
            <Button variant="outline" onClick={() => setStep((step - 1) as Step)}>
              上一步
            </Button>
          )}
          <div className="flex-1" />
          <Button variant="outline" onClick={onClose}>取消</Button>
          {step < 4 ? (
            <Button onClick={() => setStep((step + 1) as Step)} disabled={!canNext()}>
              下一步
            </Button>
          ) : (
            <Button onClick={handleCreate} disabled={submitting || !title.trim()}>
              {submitting && <Loader2 className="size-4 mr-2 animate-spin" />}
              创建项目
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
