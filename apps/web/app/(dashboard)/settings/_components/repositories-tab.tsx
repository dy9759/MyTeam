"use client";

import { useEffect, useState } from "react";
import { Save, Plus, Trash2 } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { toast } from "sonner";
import { useWorkspaceManagement } from "@/features/workspace";
import type { WorkspaceRepo } from "@/shared/types";

export function RepositoriesTab() {
  const { workspace, canManageWorkspace, saveWorkspace } = useWorkspaceManagement();

  const [repos, setRepos] = useState<WorkspaceRepo[]>(workspace?.repos ?? []);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setRepos(workspace?.repos ?? []);
  }, [workspace]);

  const handleSave = async () => {
    if (!workspace) return;
    setSaving(true);
    try {
      await saveWorkspace({ repos });
      toast.success("仓库已保存");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "保存仓库失败");
    } finally {
      setSaving(false);
    }
  };

  const handleAddRepo = () => {
    setRepos([...repos, { url: "", description: "" }]);
  };

  const handleRemoveRepo = (index: number) => {
    setRepos(repos.filter((_, i) => i !== index));
  };

  const handleRepoChange = (index: number, field: keyof WorkspaceRepo, value: string) => {
    setRepos(repos.map((r, i) => (i === index ? { ...r, [field]: value } : r)));
  };

  if (!workspace) return null;

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">代码仓库</h2>

        <Card>
          <CardContent className="space-y-3">
            <p className="text-xs text-muted-foreground">
              与此工作区关联的 GitHub 仓库。Agent 使用这些仓库来克隆和处理代码。
            </p>

            {repos.map((repo, index) => (
              <div key={index} className="flex gap-2">
                <div className="flex-1 space-y-1.5">
                  <Input
                    type="url"
                    value={repo.url}
                    onChange={(e) => handleRepoChange(index, "url", e.target.value)}
                    disabled={!canManageWorkspace}
                    placeholder="https://github.com/org/repo"
                    className="text-sm"
                  />
                  <Input
                    type="text"
                    value={repo.description}
                    onChange={(e) => handleRepoChange(index, "description", e.target.value)}
                    disabled={!canManageWorkspace}
                    placeholder="描述（例如 Go 后端 + Next.js 前端）"
                    className="text-sm"
                  />
                </div>
                {canManageWorkspace && (
                  <Button
                    variant="ghost"
                    size="icon"
                    className="mt-0.5 shrink-0 text-muted-foreground hover:text-destructive"
                    onClick={() => handleRemoveRepo(index)}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                )}
              </div>
            ))}

            {canManageWorkspace && (
              <div className="flex items-center justify-between pt-1">
                <Button variant="outline" size="sm" onClick={handleAddRepo}>
                  <Plus className="h-3 w-3" />
                  添加仓库
                </Button>
                <Button
                  size="sm"
                  onClick={handleSave}
                  disabled={saving}
                  className="bg-brand text-brand-foreground hover:opacity-90"
                >
                  <Save className="h-3 w-3" />
                  {saving ? "保存中..." : "保存"}
                </Button>
              </div>
            )}

            {!canManageWorkspace && (
              <p className="text-xs text-muted-foreground">
                仅管理员和所有者可以管理仓库。
              </p>
            )}
          </CardContent>
        </Card>
      </section>
    </div>
  );
}
