"use client";

import { useEffect, useMemo, useState } from "react";
import { Save } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { useWorkspaceManagement } from "@/features/workspace";

type PolicyField =
  | {
      id: string;
      label: string;
      description: string;
      type: "text" | "number";
      placeholder?: string;
    }
  | {
      id: string;
      label: string;
      description: string;
      type: "select";
      options: Array<{ value: string; label: string }>;
    }
  | {
      id: string;
      label: string;
      description: string;
      type: "switch";
    };

interface PolicyTabProps {
  scope: "session_policies" | "project_policies" | "file_policies";
  title: string;
  description: string;
  fields: PolicyField[];
}

function defaultValueForField(field: PolicyField): string | boolean {
  if (field.type === "switch") return false;
  if (field.type === "select") return field.options[0]?.value ?? "";
  return "";
}

export function PolicyTab({ scope, title, description, fields }: PolicyTabProps) {
  const { workspace, canManageWorkspace, saveWorkspaceSettings } = useWorkspaceManagement();
  const [saving, setSaving] = useState(false);
  const [values, setValues] = useState<Record<string, string | boolean>>({});

  const defaultValues = useMemo(
    () =>
      fields.reduce<Record<string, string | boolean>>((acc, field) => {
        acc[field.id] = defaultValueForField(field);
        return acc;
      }, {}),
    [fields],
  );

  useEffect(() => {
    const next = { ...defaultValues };
    const source = ((workspace?.settings ?? {}) as Record<string, unknown>)[scope] as Record<string, unknown> | undefined;
    if (source) {
      for (const field of fields) {
        const value = source[field.id];
        if (typeof value === "string" || typeof value === "boolean") {
          next[field.id] = value;
        } else if (typeof value === "number") {
          next[field.id] = String(value);
        }
      }
    }
    setValues(next);
  }, [defaultValues, fields, scope, workspace]);

  const handleSave = async () => {
    if (!workspace) return;
    setSaving(true);
    try {
      await saveWorkspaceSettings((currentSettings) => ({
        ...currentSettings,
        [scope]: values,
      }));
      toast.success(`${title}已保存`);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : `保存${title}失败`);
    } finally {
      setSaving(false);
    }
  };

  if (!workspace) return null;

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <div>
          <h2 className="text-sm font-semibold">{title}</h2>
          <p className="mt-1 text-sm text-muted-foreground">{description}</p>
        </div>

        <Card>
          <CardContent className="space-y-4">
            {fields.map((field) => (
              <div key={field.id} className="space-y-2">
                <div className="flex items-start justify-between gap-4">
                  <div className="space-y-1">
                    <Label className="text-xs text-muted-foreground">{field.label}</Label>
                    <p className="text-xs text-muted-foreground">{field.description}</p>
                  </div>

                  {field.type === "switch" ? (
                    <Switch
                      checked={Boolean(values[field.id])}
                      onCheckedChange={(checked) =>
                        setValues((current) => ({ ...current, [field.id]: checked }))
                      }
                      disabled={!canManageWorkspace}
                    />
                  ) : null}
                </div>

                {field.type === "text" || field.type === "number" ? (
                  <Input
                    type={field.type}
                    value={String(values[field.id] ?? "")}
                    onChange={(event) =>
                      setValues((current) => ({ ...current, [field.id]: event.target.value }))
                    }
                    disabled={!canManageWorkspace}
                    placeholder={field.placeholder}
                  />
                ) : null}

                {field.type === "select" ? (
                  <select
                    value={String(values[field.id] ?? "")}
                    onChange={(event) =>
                      setValues((current) => ({ ...current, [field.id]: event.target.value }))
                    }
                    disabled={!canManageWorkspace}
                    className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm text-foreground"
                  >
                    {field.options.map((option) => (
                      <option key={option.value} value={option.value}>
                        {option.label}
                      </option>
                    ))}
                  </select>
                ) : null}
              </div>
            ))}

            <div className="flex items-center justify-end gap-2 pt-2">
              <Button size="sm" onClick={handleSave} disabled={saving || !canManageWorkspace}>
                <Save className="h-3 w-3" />
                {saving ? "保存中..." : "保存策略"}
              </Button>
            </div>

            {!canManageWorkspace ? (
              <p className="text-xs text-muted-foreground">仅管理员和所有者可以修改策略。</p>
            ) : null}
          </CardContent>
        </Card>
      </section>
    </div>
  );
}
