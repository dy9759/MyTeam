"use client";

import { useCallback, useEffect, useState } from "react";
import { Eye, EyeOff, KeyRound, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";

import { api } from "@/shared/api";
import { useWorkspaceStore } from "@/features/workspace";
import type { WorkspaceSecretMeta } from "@/shared/types";
import { getSettingsErrorMessage } from "@/shared/settings-error";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@/components/ui/tooltip";

export function SecretsTab() {
  const workspace = useWorkspaceStore((s) => s.workspace);
  const [secrets, setSecrets] = useState<WorkspaceSecretMeta[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [revealedValues, setRevealedValues] = useState<Record<string, string>>({});
  const [revealing, setRevealing] = useState<string | null>(null);
  const [deleteConfirmKey, setDeleteConfirmKey] = useState<string | null>(null);
  const [deletingKey, setDeletingKey] = useState<string | null>(null);

  const reload = useCallback(async () => {
    if (!workspace) return;
    try {
      const list = await api.listWorkspaceSecrets(workspace.id);
      setSecrets(list);
      setError(null);
    } catch (e) {
      setError(getSettingsErrorMessage(e, "加载密钥失败"));
    } finally {
      setLoading(false);
    }
  }, [workspace]);

  useEffect(() => {
    reload();
  }, [reload]);

  const reveal = async (key: string) => {
    if (!workspace) return;
    setRevealing(key);
    try {
      const { value } = await api.getWorkspaceSecret(workspace.id, key);
      setRevealedValues((m) => ({ ...m, [key]: value }));
    } catch (e) {
      toast.error(getSettingsErrorMessage(e, "查看密钥失败"));
    } finally {
      setRevealing(null);
    }
  };

  const hide = (key: string) =>
    setRevealedValues((m) => {
      const c = { ...m };
      delete c[key];
      return c;
    });

  const handleDelete = async (key: string) => {
    if (!workspace) return;
    setDeletingKey(key);
    try {
      await api.deleteWorkspaceSecret(workspace.id, key);
      hide(key);
      await reload();
      toast.success(`密钥 "${key}" 已删除`);
    } catch (e) {
      toast.error(getSettingsErrorMessage(e, "删除密钥失败"));
    } finally {
      setDeletingKey(null);
      setDeleteConfirmKey(null);
    }
  };

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <div className="flex items-center gap-2">
          <KeyRound className="h-4 w-4 text-muted-foreground" />
          <h2 className="text-sm font-semibold">工作区密钥</h2>
        </div>

        <p className="text-xs text-muted-foreground">
          工作区密钥用于授权外部集成（例如 API Key），仅工作区管理员或所有者可访问。
        </p>

        <NewSecretForm
          workspaceId={workspace?.id ?? ""}
          onCreated={reload}
        />

        {error && <p className="text-destructive text-xs">{error}</p>}

        {loading ? (
          <div className="space-y-2">
            {Array.from({ length: 2 }).map((_, i) => (
              <Card key={i}>
                <CardContent className="flex items-center gap-3">
                  <div className="flex-1 space-y-1.5">
                    <Skeleton className="h-4 w-32" />
                    <Skeleton className="h-3 w-48" />
                  </div>
                  <Skeleton className="h-8 w-8 rounded" />
                </CardContent>
              </Card>
            ))}
          </div>
        ) : secrets.length === 0 ? (
          <p className="text-muted-foreground text-sm">尚未配置任何密钥。</p>
        ) : (
          <div className="space-y-2">
            {secrets.map((s) => {
              const revealed = revealedValues[s.key];
              return (
                <Card key={s.key}>
                  <CardContent className="flex items-start gap-3">
                    <div className="min-w-0 flex-1 space-y-1">
                      <div className="font-mono text-sm font-medium truncate">{s.key}</div>
                      <div className="text-xs text-muted-foreground">
                        创建于 {new Date(s.created_at).toLocaleString()}
                        {s.rotated_at && ` · 已轮换 ${new Date(s.rotated_at).toLocaleString()}`}
                      </div>
                      {revealed !== undefined && (
                        <Input
                          readOnly
                          value={revealed}
                          className="mt-2 font-mono text-xs"
                          onFocus={(e) => e.currentTarget.select()}
                        />
                      )}
                    </div>
                    <div className="flex items-center gap-1">
                      {revealed !== undefined ? (
                        <Tooltip>
                          <TooltipTrigger
                            render={
                              <Button
                                variant="ghost"
                                size="icon-sm"
                                onClick={() => hide(s.key)}
                                aria-label={`Hide ${s.key}`}
                              >
                                <EyeOff className="h-3.5 w-3.5" />
                              </Button>
                            }
                          />
                          <TooltipContent>隐藏</TooltipContent>
                        </Tooltip>
                      ) : (
                        <Tooltip>
                          <TooltipTrigger
                            render={
                              <Button
                                variant="ghost"
                                size="icon-sm"
                                onClick={() => reveal(s.key)}
                                disabled={revealing === s.key}
                                aria-label={`Reveal ${s.key}`}
                              >
                                <Eye className="h-3.5 w-3.5" />
                              </Button>
                            }
                          />
                          <TooltipContent>查看值</TooltipContent>
                        </Tooltip>
                      )}
                      <Tooltip>
                        <TooltipTrigger
                          render={
                            <Button
                              variant="ghost"
                              size="icon-sm"
                              onClick={() => setDeleteConfirmKey(s.key)}
                              disabled={deletingKey === s.key}
                              aria-label={`Delete ${s.key}`}
                            >
                              <Trash2 className="h-3.5 w-3.5" />
                            </Button>
                          }
                        />
                        <TooltipContent>删除</TooltipContent>
                      </Tooltip>
                    </div>
                  </CardContent>
                </Card>
              );
            })}
          </div>
        )}
      </section>

      <AlertDialog
        open={!!deleteConfirmKey}
        onOpenChange={(v) => {
          if (!v) setDeleteConfirmKey(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除密钥</AlertDialogTitle>
            <AlertDialogDescription>
              密钥 &quot;{deleteConfirmKey}&quot; 将被永久删除，依赖此密钥的集成将失败。此操作不可撤销。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={async () => {
                if (deleteConfirmKey) await handleDelete(deleteConfirmKey);
              }}
            >
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function NewSecretForm({
  workspaceId,
  onCreated,
}: {
  workspaceId: string;
  onCreated: () => void;
}) {
  const [key, setKey] = useState("");
  const [value, setValue] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const submit = async () => {
    const trimmedKey = key.trim();
    if (!trimmedKey || !value || !workspaceId) return;
    setSubmitting(true);
    try {
      await api.setWorkspaceSecret(workspaceId, trimmedKey, value);
      setKey("");
      setValue("");
      onCreated();
      toast.success(`密钥 "${trimmedKey}" 已保存`);
    } catch (e) {
      toast.error(getSettingsErrorMessage(e, "保存密钥失败"));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Card>
      <CardContent className="space-y-3">
        <div className="grid gap-3 sm:grid-cols-[1fr_1fr_auto]">
          <Input
            type="text"
            value={key}
            onChange={(e) => setKey(e.target.value)}
            placeholder="名称（例如 anthropic_api_key）"
            disabled={submitting || !workspaceId}
          />
          <Input
            type="password"
            value={value}
            onChange={(e) => setValue(e.target.value)}
            placeholder="值"
            disabled={submitting || !workspaceId}
            onKeyDown={(e) => {
              if (e.key === "Enter") submit();
            }}
          />
          <Button
            onClick={submit}
            disabled={submitting || !key.trim() || !value || !workspaceId}
          >
            <Plus className="mr-1 h-4 w-4" />
            {submitting ? "保存中..." : "添加"}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
