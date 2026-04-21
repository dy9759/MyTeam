"use client";

import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { Plus, Bot, Link2, Unlink, Trash2, Globe, Building, Package } from "lucide-react";

import { api } from "@/shared/api";
import type { Skill, Subagent } from "@/shared/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

// Subagents wrap skills; agents only reach skills through them after
// migration 069. This page lists bundle + workspace subagents, lets a
// user create a workspace-local subagent, and link/unlink skills.

export default function SubagentsPage() {
  const [subagents, setSubagents] = useState<Subagent[]>([]);
  const [skills, setSkills] = useState<Skill[]>([]);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [categoryFilter, setCategoryFilter] = useState<string>("all");
  const [loading, setLoading] = useState(true);
  const [createOpen, setCreateOpen] = useState(false);

  useEffect(() => {
    Promise.all([api.listSubagents(), api.listSkills()])
      .then(([sa, sk]) => {
        setSubagents(sa);
        setSkills(sk);
      })
      .catch((e) => toast.error(e instanceof Error ? e.message : "加载失败"))
      .finally(() => setLoading(false));
  }, []);

  const categories = useMemo(() => {
    const set = new Set<string>();
    subagents.forEach((s) => set.add(s.category || "general"));
    return ["all", ...Array.from(set).sort()];
  }, [subagents]);

  const filtered = useMemo(
    () =>
      categoryFilter === "all"
        ? subagents
        : subagents.filter((s) => (s.category || "general") === categoryFilter),
    [subagents, categoryFilter],
  );

  const selected = useMemo(
    () => subagents.find((s) => s.id === selectedId) ?? null,
    [subagents, selectedId],
  );

  const reloadSelected = async (id: string) => {
    try {
      const hydrated = await api.getSubagent(id);
      setSubagents((prev) => prev.map((s) => (s.id === id ? hydrated : s)));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "刷新 subagent 失败");
    }
  };

  const handleCreate = async (body: {
    name: string;
    description: string;
    instructions: string;
    category: string;
  }) => {
    try {
      const created = await api.createSubagent(body);
      setSubagents((prev) => [...prev, created]);
      setSelectedId(created.id);
      setCreateOpen(false);
      toast.success("已创建 subagent");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "创建失败");
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm("确认删除该 subagent？已链接的技能将一并解绑。")) return;
    try {
      await api.deleteSubagent(id);
      setSubagents((prev) => prev.filter((s) => s.id !== id));
      if (selectedId === id) setSelectedId(null);
      toast.success("已删除");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "删除失败");
    }
  };

  const handleLink = async (skillId: string) => {
    if (!selected) return;
    try {
      await api.linkSubagentSkill(selected.id, skillId);
      await reloadSelected(selected.id);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "链接失败");
    }
  };

  const handleUnlink = async (skillId: string) => {
    if (!selected) return;
    try {
      await api.unlinkSubagentSkill(selected.id, skillId);
      await reloadSelected(selected.id);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "解绑失败");
    }
  };

  if (loading) {
    return (
      <div className="flex-1 flex items-center justify-center text-muted-foreground">
        加载中…
      </div>
    );
  }

  return (
    <div className="flex flex-1 min-h-0">
      {/* Left: list */}
      <div className="w-[320px] shrink-0 flex flex-col border-r border-border bg-card">
        <div className="p-3 border-b border-border flex items-center gap-2">
          <Select
            value={categoryFilter}
            onValueChange={(v) => setCategoryFilter(v ?? "all")}
          >
            <SelectTrigger className="h-8 text-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {categories.map((c) => (
                <SelectItem key={c} value={c}>
                  {c === "all" ? "全部分类" : c}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button size="sm" onClick={() => setCreateOpen(true)}>
            <Plus className="size-3.5 mr-1" />
            新建
          </Button>
        </div>
        <div className="flex-1 overflow-y-auto p-1.5 space-y-0.5">
          {filtered.length === 0 ? (
            <div className="text-center text-sm text-muted-foreground py-8">
              暂无 subagent
            </div>
          ) : (
            filtered.map((sa) => (
              <button
                key={sa.id}
                type="button"
                onClick={() => setSelectedId(sa.id)}
                className={`w-full text-left rounded-md px-3 py-2.5 transition-colors ${
                  selectedId === sa.id
                    ? "bg-muted text-foreground"
                    : "hover:bg-accent text-secondary-foreground"
                }`}
              >
                <div className="flex items-center gap-2 min-w-0">
                  <Bot className="size-3.5 shrink-0 text-muted-foreground" />
                  <span className="font-medium text-sm truncate">{sa.name}</span>
                </div>
                <div className="flex items-center gap-2 mt-1 flex-wrap">
                  <ScopeBadge subagent={sa} />
                  <span className="text-[11px] text-muted-foreground">
                    {sa.category || "general"}
                  </span>
                </div>
              </button>
            ))
          )}
        </div>
      </div>

      {/* Right: detail + skills editor */}
      <div className="flex-1 min-h-0 overflow-auto">
        {selected ? (
          <SubagentDetail
            subagent={selected}
            allSkills={skills}
            onDelete={() => handleDelete(selected.id)}
            onLink={handleLink}
            onUnlink={handleUnlink}
          />
        ) : (
          <div className="flex h-full items-center justify-center text-muted-foreground text-sm">
            选择一个 subagent
          </div>
        )}
      </div>

      {createOpen && (
        <CreateSubagentDialog onClose={() => setCreateOpen(false)} onCreate={handleCreate} />
      )}
    </div>
  );
}

function ScopeBadge({ subagent }: { subagent: Subagent }) {
  if (subagent.source === "bundle") {
    return (
      <Badge variant="outline" className="text-[10px] gap-1">
        <Package className="size-3" /> bundle
      </Badge>
    );
  }
  if (subagent.is_global) {
    return (
      <Badge variant="outline" className="text-[10px] gap-1">
        <Globe className="size-3" /> global
      </Badge>
    );
  }
  return (
    <Badge variant="outline" className="text-[10px] gap-1">
      <Building className="size-3" /> workspace
    </Badge>
  );
}

function SubagentDetail({
  subagent,
  allSkills,
  onDelete,
  onLink,
  onUnlink,
}: {
  subagent: Subagent;
  allSkills: Skill[];
  onDelete: () => void;
  onLink: (skillId: string) => Promise<void>;
  onUnlink: (skillId: string) => Promise<void>;
}) {
  const linked = subagent.skills ?? [];
  const linkedIds = new Set(linked.map((s) => s.id));
  const available = allSkills.filter((s) => !linkedIds.has(s.id));
  const readOnly = subagent.source === "bundle";

  return (
    <div className="p-6 space-y-6">
      <div>
        <div className="flex items-center gap-3">
          <h1 className="text-2xl font-semibold">{subagent.name}</h1>
          <ScopeBadge subagent={subagent} />
          <Badge variant="secondary" className="text-[10px]">
            {subagent.category || "general"}
          </Badge>
          {!readOnly && (
            <Button
              variant="outline"
              size="sm"
              className="ml-auto text-destructive border-destructive/40 hover:bg-destructive/10"
              onClick={onDelete}
            >
              <Trash2 className="size-3.5 mr-1" />
              删除
            </Button>
          )}
        </div>
        {subagent.description && (
          <p className="text-sm text-muted-foreground mt-2 whitespace-pre-wrap">
            {subagent.description}
          </p>
        )}
      </div>

      <section>
        <h2 className="text-sm font-medium mb-2">指令（system prompt）</h2>
        <div className="border border-border rounded-md bg-muted/30 p-3 text-sm whitespace-pre-wrap font-mono max-h-[320px] overflow-auto">
          {subagent.instructions || "(empty)"}
        </div>
      </section>

      <section>
        <div className="flex items-center justify-between mb-2">
          <h2 className="text-sm font-medium">已链接技能 ({linked.length})</h2>
          {readOnly && (
            <span className="text-xs text-muted-foreground">
              Bundle subagent — 技能由 bundle 配置管理，只读
            </span>
          )}
        </div>
        {linked.length === 0 ? (
          <div className="text-sm text-muted-foreground border border-dashed rounded-md py-6 text-center">
            尚未链接任何技能
          </div>
        ) : (
          <ul className="space-y-1.5">
            {linked.map((s) => (
              <li
                key={s.id}
                className="flex items-center gap-2 border border-border rounded-md px-3 py-2 bg-card"
              >
                <div className="flex-1 min-w-0">
                  <div className="font-medium text-sm truncate">{s.name}</div>
                  {s.description && (
                    <div className="text-xs text-muted-foreground line-clamp-2">
                      {s.description}
                    </div>
                  )}
                </div>
                <Badge variant="outline" className="text-[10px]">
                  {s.category || "general"}
                </Badge>
                {!readOnly && (
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => onUnlink(s.id)}
                    className="text-muted-foreground"
                  >
                    <Unlink className="size-3.5" />
                  </Button>
                )}
              </li>
            ))}
          </ul>
        )}
      </section>

      {!readOnly && (
        <section>
          <h2 className="text-sm font-medium mb-2">可添加技能</h2>
          {available.length === 0 ? (
            <div className="text-sm text-muted-foreground">无可添加技能</div>
          ) : (
            <ul className="space-y-1.5">
              {available.map((s) => (
                <li
                  key={s.id}
                  className="flex items-center gap-2 border border-border rounded-md px-3 py-2 bg-card"
                >
                  <div className="flex-1 min-w-0">
                    <div className="font-medium text-sm truncate">{s.name}</div>
                    {s.description && (
                      <div className="text-xs text-muted-foreground line-clamp-2">
                        {s.description}
                      </div>
                    )}
                  </div>
                  <Badge variant="outline" className="text-[10px]">
                    {s.category || "general"}
                  </Badge>
                  {s.is_global && (
                    <Badge variant="outline" className="text-[10px] gap-1">
                      <Globe className="size-3" /> global
                    </Badge>
                  )}
                  <Button size="sm" variant="outline" onClick={() => onLink(s.id)}>
                    <Link2 className="size-3.5 mr-1" />
                    链接
                  </Button>
                </li>
              ))}
            </ul>
          )}
        </section>
      )}
    </div>
  );
}

function CreateSubagentDialog({
  onClose,
  onCreate,
}: {
  onClose: () => void;
  onCreate: (body: {
    name: string;
    description: string;
    instructions: string;
    category: string;
  }) => Promise<void>;
}) {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [instructions, setInstructions] = useState("");
  const [category, setCategory] = useState("general");
  const [submitting, setSubmitting] = useState(false);

  const submit = async () => {
    if (!name.trim()) return;
    setSubmitting(true);
    try {
      await onCreate({
        name: name.trim(),
        description: description.trim(),
        instructions: instructions.trim(),
        category: category.trim() || "general",
      });
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open onOpenChange={onClose}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>新建 Subagent</DialogTitle>
        </DialogHeader>
        <div className="space-y-3">
          <div>
            <Label className="text-xs">名称 *</Label>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              autoFocus
              className="mt-1"
              placeholder="如 Code Reviewer"
            />
          </div>
          <div>
            <Label className="text-xs">分类</Label>
            <Input
              value={category}
              onChange={(e) => setCategory(e.target.value)}
              className="mt-1"
              placeholder="general, engineering, research ..."
            />
          </div>
          <div>
            <Label className="text-xs">描述</Label>
            <Textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              className="mt-1"
              rows={2}
            />
          </div>
          <div>
            <Label className="text-xs">指令（system prompt）</Label>
            <Textarea
              value={instructions}
              onChange={(e) => setInstructions(e.target.value)}
              className="mt-1 font-mono text-sm"
              rows={6}
              placeholder="You are ..."
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            取消
          </Button>
          <Button onClick={submit} disabled={!name.trim() || submitting}>
            创建
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
