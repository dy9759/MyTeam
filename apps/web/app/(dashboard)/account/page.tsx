"use client";

import { useEffect, useState, useCallback } from "react";
import { useRouter } from "next/navigation";
import { useAuthStore } from "@/features/auth";
import { useWorkspaceStore } from "@/features/workspace";
import { api } from "@/shared/api";
import type { Agent, IdentityCard, Project } from "@/shared/types";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Textarea } from "@/components/ui/textarea";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { Bot, ChevronDown, Sparkles, Pencil, UserCheck, FolderGit2 } from "lucide-react";
import { toast } from "sonner";

export default function AccountPage() {
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const agents = useWorkspaceStore((s) => s.agents);
  const refreshAgents = useWorkspaceStore((s) => s.refreshAgents);

  // Track which agents have identity cards expanded
  const [expandedCards, setExpandedCards] = useState<Set<string>>(new Set());
  // Track editing state per agent
  const [editingAgent, setEditingAgent] = useState<string | null>(null);
  const [editDescription, setEditDescription] = useState("");
  // Track generated identity card preview
  const [generatingAgent, setGeneratingAgent] = useState<string | null>(null);
  const [previewCard, setPreviewCard] = useState<{ agentId: string; card: IdentityCard } | null>(null);
  // Active projects
  const [activeProjects, setActiveProjects] = useState<Project[]>([]);

  useEffect(() => {
    refreshAgents();
  }, [refreshAgents]);

  useEffect(() => {
    api.listProjects().then((projects) => {
      const active = projects.filter(
        (p) => p.status === "running" || p.status === "not_started" || p.status === "paused",
      );
      setActiveProjects(active);
    }).catch(() => {});
  }, []);

  const myAgents = agents.filter((a) => a.owner_id === user?.id && !a.archived_at);

  const toggleExpanded = useCallback((agentId: string) => {
    setExpandedCards((prev) => {
      const next = new Set(prev);
      if (next.has(agentId)) next.delete(agentId);
      else next.add(agentId);
      return next;
    });
  }, []);

  async function handleToggleOnline(agent: Agent) {
    const newStatus = agent.online_status === "online" ? "offline" : "online";
    try {
      await api.updateAgentStatus(agent.id, { online_status: newStatus });
      useWorkspaceStore.getState().updateAgent(agent.id, { online_status: newStatus });
      toast.success(`Agent ${newStatus === "online" ? "online" : "offline"}`);
    } catch {
      toast.error("Failed to update agent status");
    }
  }

  async function handleGenerateIdentity(agentId: string) {
    setGeneratingAgent(agentId);
    try {
      const card = await api.generateIdentityCard(agentId);
      setPreviewCard({ agentId, card });
    } catch {
      toast.error("Failed to generate identity card");
    } finally {
      setGeneratingAgent(null);
    }
  }

  async function handleSaveGeneratedCard() {
    if (!previewCard) return;
    try {
      await api.updateIdentityCard(previewCard.agentId, previewCard.card);
      useWorkspaceStore.getState().updateAgent(previewCard.agentId, {
        identity_card: previewCard.card,
      });
      toast.success("Identity card saved");
      setPreviewCard(null);
    } catch {
      toast.error("Failed to save identity card");
    }
  }

  async function handleSaveManualDescription(agentId: string) {
    try {
      await api.updateIdentityCard(agentId, { description_manual: editDescription });
      const current = agents.find((a) => a.id === agentId)?.identity_card;
      useWorkspaceStore.getState().updateAgent(agentId, {
        identity_card: { ...emptyIdentityCard, ...current, description_manual: editDescription },
      });
      toast.success("Description updated");
      setEditingAgent(null);
    } catch {
      toast.error("Failed to update description");
    }
  }

  function handleImpersonate(agentId: string) {
    localStorage.setItem("multica_impersonate_agent", agentId);
    window.location.reload();
  }

  const workloadStatusVariant = (status?: string): "default" | "secondary" | "destructive" | "outline" => {
    switch (status) {
      case "idle": return "secondary";
      case "busy": return "default";
      case "blocked": return "destructive";
      case "degraded": return "destructive";
      case "suspended": return "outline";
      default: return "secondary";
    }
  };

  return (
    <div className="p-6 max-w-4xl mx-auto space-y-6">
      <h1 className="text-2xl font-bold">Account</h1>

      {/* Owner Profile Card */}
      <Card>
        <div className="h-20 bg-gradient-to-r from-primary/30 to-primary/10 rounded-t-lg" />
        <CardContent className="-mt-8 pb-6">
          <div className="flex items-end gap-4 mb-4">
            <Avatar className="h-16 w-16 border-4 border-background">
              <AvatarFallback className="text-2xl bg-muted">
                {user?.name?.[0]?.toUpperCase() ?? "U"}
              </AvatarFallback>
            </Avatar>
            <div className="pb-1">
              <h2 className="text-xl font-bold">{user?.name ?? "Loading..."}</h2>
              <div className="text-sm text-muted-foreground">{user?.email}</div>
            </div>
          </div>
          <div className="flex items-center gap-3 text-sm text-muted-foreground">
            <Badge variant="secondary">Owner</Badge>
            <div className="flex items-center gap-1.5">
              <span className="h-2 w-2 rounded-full bg-emerald-500" />
              Online
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Agent Cards */}
      <div>
        <h2 className="text-lg font-semibold mb-4">
          My Agents ({myAgents.length})
        </h2>

        {myAgents.length === 0 ? (
          <Card>
            <CardContent className="py-8 text-center text-muted-foreground">
              No agents yet. Create an agent from the Agents page.
            </CardContent>
          </Card>
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            {myAgents.map((agent) => {
              const ic = agent.identity_card;
              const isExpanded = expandedCards.has(agent.id);

              return (
                <Card key={agent.id} className="overflow-hidden">
                  <CardHeader className="pb-3">
                    <div className="flex items-start gap-3">
                      <Avatar className="h-10 w-10">
                        <AvatarFallback className="bg-primary/10 text-primary">
                          <Bot className="h-5 w-5" />
                        </AvatarFallback>
                      </Avatar>
                      <div className="flex-1 min-w-0">
                        <div className="flex items-center gap-2">
                          <CardTitle className="text-base truncate">
                            {agent.name}
                          </CardTitle>
                          <Badge variant="outline" className="text-xs shrink-0">
                            {agent.agent_type === "system_agent"
                              ? "System"
                              : agent.agent_type === "page_system_agent"
                                ? "Page"
                                : "Personal"}
                          </Badge>
                        </div>
                        <div className="flex items-center gap-2 mt-1">
                          <span
                            className={`h-2 w-2 rounded-full ${
                              agent.online_status === "online"
                                ? "bg-emerald-500"
                                : "bg-muted-foreground/40"
                            }`}
                          />
                          <span className="text-xs text-muted-foreground">
                            {agent.online_status === "online" ? "Online" : "Offline"}
                          </span>
                          {agent.workload_status && (
                            <Badge
                              variant={workloadStatusVariant(agent.workload_status)}
                              className="text-xs"
                            >
                              {agent.workload_status}
                            </Badge>
                          )}
                        </div>
                      </div>
                    </div>
                  </CardHeader>

                  <CardContent className="space-y-3">
                    {/* Identity Card (collapsible) */}
                    <Collapsible open={isExpanded} onOpenChange={() => toggleExpanded(agent.id)}>
                      <CollapsibleTrigger className="flex items-center gap-1 text-sm font-medium text-muted-foreground hover:text-foreground w-full">
                        <ChevronDown
                          className={`h-4 w-4 transition-transform ${isExpanded ? "" : "-rotate-90"}`}
                        />
                        Identity Card
                      </CollapsibleTrigger>
                      <CollapsibleContent className="mt-2 space-y-3">
                        {/* Capabilities */}
                        {ic?.capabilities && ic.capabilities.length > 0 && (
                          <div>
                            <div className="text-xs text-muted-foreground mb-1">Capabilities</div>
                            <div className="flex flex-wrap gap-1">
                              {ic.capabilities.map((c) => (
                                <Badge key={c} variant="secondary" className="text-xs">
                                  {c}
                                </Badge>
                              ))}
                            </div>
                          </div>
                        )}

                        {/* Skills */}
                        {ic?.skills && ic.skills.length > 0 && (
                          <div>
                            <div className="text-xs text-muted-foreground mb-1">Skills</div>
                            <div className="flex flex-wrap gap-1">
                              {ic.skills.map((s) => (
                                <Badge key={s} variant="outline" className="text-xs">
                                  {s}
                                </Badge>
                              ))}
                            </div>
                          </div>
                        )}

                        {/* Tools */}
                        {ic?.tools && ic.tools.length > 0 && (
                          <div>
                            <div className="text-xs text-muted-foreground mb-1">Tools</div>
                            <div className="flex flex-wrap gap-1">
                              {ic.tools.map((t) => (
                                <Badge key={t} variant="outline" className="text-xs">
                                  {t}
                                </Badge>
                              ))}
                            </div>
                          </div>
                        )}

                        {/* Completed Projects */}
                        {ic?.completed_projects && ic.completed_projects.length > 0 && (
                          <div className="text-xs text-muted-foreground">
                            Completed projects: {ic.completed_projects.length}
                          </div>
                        )}

                        {/* Auto Description */}
                        {ic?.description_auto && (
                          <div>
                            <div className="text-xs text-muted-foreground mb-1">Auto Description</div>
                            <p className="text-sm">{ic.description_auto}</p>
                          </div>
                        )}

                        {/* Manual Description */}
                        <div>
                          <div className="text-xs text-muted-foreground mb-1">Manual Description</div>
                          {editingAgent === agent.id ? (
                            <div className="space-y-2">
                              <Textarea
                                value={editDescription}
                                onChange={(e) => setEditDescription(e.target.value)}
                                rows={3}
                                placeholder="Enter a description for this agent..."
                              />
                              <div className="flex gap-2">
                                <Button
                                  size="sm"
                                  onClick={() => handleSaveManualDescription(agent.id)}
                                >
                                  Save
                                </Button>
                                <Button
                                  size="sm"
                                  variant="outline"
                                  onClick={() => setEditingAgent(null)}
                                >
                                  Cancel
                                </Button>
                              </div>
                            </div>
                          ) : (
                            <p className="text-sm">
                              {ic?.description_manual || (
                                <span className="text-muted-foreground italic">
                                  No manual description
                                </span>
                              )}
                            </p>
                          )}
                        </div>

                        {/* Preview generated card */}
                        {previewCard?.agentId === agent.id && (
                          <Card className="border-dashed">
                            <CardHeader className="pb-2">
                              <CardTitle className="text-sm">Generated Identity Preview</CardTitle>
                            </CardHeader>
                            <CardContent className="space-y-2 text-xs">
                              {previewCard.card.capabilities.length > 0 && (
                                <div>
                                  <span className="text-muted-foreground">Capabilities: </span>
                                  {previewCard.card.capabilities.join(", ")}
                                </div>
                              )}
                              {previewCard.card.skills.length > 0 && (
                                <div>
                                  <span className="text-muted-foreground">Skills: </span>
                                  {previewCard.card.skills.join(", ")}
                                </div>
                              )}
                              {previewCard.card.description_auto && (
                                <div>
                                  <span className="text-muted-foreground">Description: </span>
                                  {previewCard.card.description_auto}
                                </div>
                              )}
                              <div className="flex gap-2 pt-1">
                                <Button size="sm" onClick={handleSaveGeneratedCard}>
                                  Save
                                </Button>
                                <Button
                                  size="sm"
                                  variant="outline"
                                  onClick={() => setPreviewCard(null)}
                                >
                                  Discard
                                </Button>
                              </div>
                            </CardContent>
                          </Card>
                        )}
                      </CollapsibleContent>
                    </Collapsible>

                    {/* Actions */}
                    <div className="flex flex-wrap gap-2 pt-2 border-t">
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => handleGenerateIdentity(agent.id)}
                        disabled={generatingAgent === agent.id}
                      >
                        <Sparkles className="h-3.5 w-3.5 mr-1" />
                        {generatingAgent === agent.id ? "Generating..." : "Generate Identity"}
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => {
                          setEditingAgent(agent.id);
                          setEditDescription(ic?.description_manual ?? "");
                          if (!expandedCards.has(agent.id)) toggleExpanded(agent.id);
                        }}
                      >
                        <Pencil className="h-3.5 w-3.5 mr-1" />
                        Edit Identity
                      </Button>
                      <div className="flex items-center gap-2 ml-auto">
                        <span className="text-xs text-muted-foreground">Online</span>
                        <Switch
                          checked={agent.online_status === "online"}
                          onCheckedChange={() => handleToggleOnline(agent)}
                        />
                      </div>
                    </div>
                    <Button
                      size="sm"
                      variant="outline"
                      className="w-full"
                      onClick={() => handleImpersonate(agent.id)}
                    >
                      <UserCheck className="h-3.5 w-3.5 mr-1" />
                      Impersonate
                    </Button>
                  </CardContent>
                </Card>
              );
            })}
          </div>
        )}
      </div>

      {/* Active Projects */}
      <div>
        <h2 className="text-lg font-semibold mb-4">
          Active Projects ({activeProjects.length})
        </h2>

        {activeProjects.length === 0 ? (
          <Card>
            <CardContent className="py-8 text-center text-muted-foreground">
              No active projects.
            </CardContent>
          </Card>
        ) : (
          <div className="space-y-2">
            {activeProjects.map((project) => {
              const projectStatusVariant = (status: string): "default" | "secondary" | "destructive" | "outline" => {
                switch (status) {
                  case "running": return "default";
                  case "paused": return "outline";
                  case "not_started": return "secondary";
                  case "completed": return "secondary";
                  case "failed": return "destructive";
                  default: return "secondary";
                }
              };

              return (
                <Card
                  key={project.id}
                  className="cursor-pointer hover:bg-muted/50 transition-colors"
                  onClick={() => router.push(`/projects/${project.id}`)}
                >
                  <CardContent className="flex items-center gap-3 py-3">
                    <FolderGit2 className="h-5 w-5 text-muted-foreground shrink-0" />
                    <div className="flex-1 min-w-0">
                      <div className="font-medium truncate">{project.title}</div>
                      {project.description && (
                        <div className="text-xs text-muted-foreground truncate">
                          {project.description}
                        </div>
                      )}
                    </div>
                    <Badge variant={projectStatusVariant(project.status)}>
                      {project.status}
                    </Badge>
                  </CardContent>
                </Card>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}

const emptyIdentityCard: IdentityCard = {
  capabilities: [],
  tools: [],
  skills: [],
  subagents: [],
  completed_projects: [],
  description_auto: "",
  description_manual: "",
};
