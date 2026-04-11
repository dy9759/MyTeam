"use client";

import type { Agent, IdentityCard } from "@/shared/types";

export const emptyCard: IdentityCard = {
  title: "",
  capabilities: [],
  tools: [],
  skills: [],
  subagents: [],
  completed_projects: [],
  description_auto: "",
  description_manual: "",
  pinned_fields: [],
  visibility: "workspace",
  needs_attention: false,
};

export const statusMeta: Record<string, { label: string; dot: string }> = {
  idle: { label: "空闲", dot: "bg-emerald-500" },
  working: { label: "工作中", dot: "bg-primary" },
  blocked: { label: "阻塞", dot: "bg-amber-500" },
  degraded: { label: "降级", dot: "bg-yellow-500" },
  suspended: { label: "暂停", dot: "bg-muted-foreground/60" },
  offline: { label: "离线", dot: "bg-muted-foreground/40" },
  error: { label: "错误", dot: "bg-destructive" },
};

export function cardFor(agent?: Agent | null): IdentityCard {
  if (!agent?.identity_card) return emptyCard;
  return {
    ...emptyCard,
    ...agent.identity_card,
    capabilities: agent.identity_card.capabilities ?? [],
    tools: agent.identity_card.tools ?? [],
    skills: agent.identity_card.skills ?? [],
    subagents: agent.identity_card.subagents ?? [],
    completed_projects: agent.identity_card.completed_projects ?? [],
    pinned_fields: agent.identity_card.pinned_fields ?? [],
  };
}

export function splitList(value: string): string[] {
  return value
    .split(/[,，\n]/)
    .map((item) => item.trim())
    .filter(Boolean);
}

export function formatAuditAction(action: string) {
  return action.replace(/^activity:/, "").replace(/_/g, " ");
}

export function formatTime(value: string) {
  return new Date(value).toLocaleString();
}
