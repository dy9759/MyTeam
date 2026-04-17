"use client";

import { useEffect, useState, useCallback } from "react";
import type { RemoteSession } from "@/shared/types";
import type { Agent } from "@/shared/types";
import { api } from "@/shared/api";
import { useWorkspaceStore } from "@/features/workspace";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogTrigger,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
  DialogClose,
} from "@/components/ui/dialog";
import { Plus, ChevronDown, ChevronRight, Monitor, Clock } from "lucide-react";
import { toast } from "sonner";

const STATUS_VARIANT: Record<string, "default" | "secondary" | "destructive" | "outline"> = {
  active: "default",
  completed: "secondary",
  failed: "destructive",
  pending: "outline",
};

export function RemoteSessionsList() {
  const [remoteSessions, setRemoteSessions] = useState<RemoteSession[]>([]);
  const [loading, setLoading] = useState(true);
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [expandedSession, setExpandedSession] = useState<RemoteSession | null>(null);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [selectedAgentId, setSelectedAgentId] = useState("");
  const [title, setTitle] = useState("");
  const [creating, setCreating] = useState(false);
  const agents = useWorkspaceStore((s) => s.agents);

  const fetchRemoteSessions = useCallback(async () => {
    try {
      const sessions = await api.listRemoteSessions();
      setRemoteSessions(sessions ?? []);
    } catch {
      toast.error("Failed to load remote sessions");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchRemoteSessions();
  }, [fetchRemoteSessions]);

  async function handleExpand(id: string) {
    if (expandedId === id) {
      setExpandedId(null);
      setExpandedSession(null);
      return;
    }
    setExpandedId(id);
    try {
      const session = await api.getRemoteSession(id);
      setExpandedSession(session);
    } catch {
      toast.error("Failed to load session details");
    }
  }

  async function handleCreate() {
    if (!selectedAgentId) return;
    setCreating(true);
    try {
      const session = await api.createRemoteSession({
        agent_id: selectedAgentId,
        title: title.trim() || undefined,
      });
      setRemoteSessions((prev) => [session, ...prev]);
      setDialogOpen(false);
      setSelectedAgentId("");
      setTitle("");
      toast.success("Remote session created");
    } catch {
      toast.error("Failed to create remote session");
    } finally {
      setCreating(false);
    }
  }

  function getAgentName(agentId: string): string {
    const agent = agents.find((a: Agent) => a.id === agentId);
    return agent?.name ?? agentId.slice(0, 12);
  }

  if (loading) {
    return (
      <div className="p-6">
        <div className="text-center py-12 text-muted-foreground">Loading remote sessions...</div>
      </div>
    );
  }

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <div className="flex items-center gap-2">
          <Monitor className="size-5 text-muted-foreground" />
          <h2 className="text-lg font-semibold">Remote Sessions</h2>
        </div>

        <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
          <DialogTrigger
            render={
              <Button size="sm">
                <Plus className="size-4 mr-1" />
                New Remote Session
              </Button>
            }
          />
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Create Remote Session</DialogTitle>
              <DialogDescription>
                Start a new remote session to track an agent&apos;s work on another machine.
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-4 py-4">
              <div>
                <label className="text-sm font-medium mb-1 block">Agent</label>
                <select
                  value={selectedAgentId}
                  onChange={(e) => setSelectedAgentId(e.target.value)}
                  className="w-full px-3 py-2 border rounded-md text-sm bg-background"
                >
                  <option value="">Select an agent...</option>
                  {agents.map((agent: Agent) => (
                    <option key={agent.id} value={agent.id}>
                      {agent.name}
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <label className="text-sm font-medium mb-1 block">Title (optional)</label>
                <Input
                  value={title}
                  onChange={(e) => setTitle(e.target.value)}
                  placeholder="Session title"
                />
              </div>
            </div>
            <DialogFooter>
              <DialogClose render={<Button variant="outline">Cancel</Button>} />
              <Button onClick={handleCreate} disabled={!selectedAgentId || creating}>
                {creating ? "Creating..." : "Create"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      {remoteSessions.length === 0 ? (
        <div className="text-center py-12">
          <p className="text-muted-foreground mb-2">No remote sessions</p>
          <p className="text-sm text-muted-foreground">
            Create a remote session to track agent work on another machine.
          </p>
        </div>
      ) : (
        <div className="space-y-2">
          {remoteSessions.map((session) => (
            <div key={session.id} className="border rounded-lg overflow-hidden">
              <div
                onClick={() => handleExpand(session.id)}
                className="p-4 flex items-center gap-3 hover:bg-muted/50 cursor-pointer"
              >
                {expandedId === session.id ? (
                  <ChevronDown className="size-4 text-muted-foreground" />
                ) : (
                  <ChevronRight className="size-4 text-muted-foreground" />
                )}
                <div className="flex-1 min-w-0">
                  <div className="font-medium truncate">
                    {session.title || "Untitled Session"}
                  </div>
                  <div className="text-xs text-muted-foreground mt-0.5">
                    Agent: {getAgentName(session.agent_id)}
                  </div>
                </div>
                <Badge variant={STATUS_VARIANT[session.status] ?? "outline"}>
                  {session.status}
                </Badge>
                <div className="text-xs text-muted-foreground flex items-center gap-1">
                  <Clock className="size-3" />
                  {new Date(session.created_at).toLocaleDateString()}
                </div>
              </div>

              {/* Expanded event timeline */}
              {expandedId === session.id && expandedSession && (
                <div className="border-t px-4 py-3 bg-muted/30">
                  {expandedSession.events && expandedSession.events.length > 0 ? (
                    <div className="space-y-2">
                      <div className="text-xs font-medium text-muted-foreground mb-2">Event Timeline</div>
                      {expandedSession.events.map((event) => (
                        <div key={event.id} className="flex gap-3 text-xs">
                          <div className="text-muted-foreground whitespace-nowrap">
                            {new Date(event.created_at).toLocaleTimeString()}
                          </div>
                          <div className="flex-1">
                            <span className="font-medium">{event.type}</span>
                            {event.data && Object.keys(event.data).length > 0 && (
                              <pre className="mt-1 text-xs bg-muted p-2 rounded overflow-auto">
                                {JSON.stringify(event.data, null, 2)}
                              </pre>
                            )}
                          </div>
                        </div>
                      ))}
                    </div>
                  ) : (
                    <div className="text-xs text-muted-foreground text-center py-2">
                      No events yet
                    </div>
                  )}
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
