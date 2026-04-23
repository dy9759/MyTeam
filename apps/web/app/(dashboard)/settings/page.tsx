"use client";

import { User, Palette, Key, KeyRound, Settings, Users, FolderGit2 } from "lucide-react";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { useWorkspaceStore } from "@/features/workspace";
import { AccountTab } from "./_components/account-tab";
import { AppearanceTab } from "./_components/general-tab";
import { TokensTab } from "./_components/tokens-tab";
import { WorkspaceTab } from "./_components/workspace-tab";
import { MembersTab } from "@/features/workspace";
import { RepositoriesTab } from "./_components/repositories-tab";
import { SecretsTab } from "./_components/secrets-tab";

const accountTabs = [
  { value: "profile", label: "个人资料", icon: User },
  { value: "appearance", label: "外观", icon: Palette },
  { value: "tokens", label: "API 令牌", icon: Key },
];

const workspaceTabs = [
  { value: "workspace", label: "通用", icon: Settings },
  { value: "repositories", label: "代码仓库", icon: FolderGit2 },
  { value: "members", label: "成员", icon: Users },
  { value: "secrets", label: "密钥", icon: KeyRound },
];

export default function SettingsPage() {
  const workspaceName = useWorkspaceStore((s) => s.workspace?.name);

  return (
    <Tabs defaultValue="profile" orientation="vertical" className="flex-1 min-h-0 gap-0 bg-background">
      {/* Left nav */}
      <div className="w-52 shrink-0 border-r border-[rgba(255,255,255,0.08)] overflow-y-auto p-4">
        <h1 className="text-sm font-semibold mb-4 px-2 text-foreground">设置</h1>
        <TabsList variant="line" className="flex-col items-stretch">
          {/* My Account group */}
          <span className="px-2 pb-1 pt-2 text-xs font-medium text-muted-foreground">
            我的账户
          </span>
          {accountTabs.map((tab) => (
            <TabsTrigger key={tab.value} value={tab.value}>
              <tab.icon className="h-4 w-4" />
              {tab.label}
            </TabsTrigger>
          ))}

          {/* Workspace group */}
          <span className="px-2 pb-1 pt-4 text-xs font-medium text-muted-foreground truncate">
            {workspaceName ?? "工作区"}
          </span>
          {workspaceTabs.map((tab) => (
            <TabsTrigger key={tab.value} value={tab.value}>
              <tab.icon className="h-4 w-4" />
              {tab.label}
            </TabsTrigger>
          ))}
        </TabsList>
      </div>

      {/* Right content */}
      <div className="flex-1 min-w-0 overflow-y-auto">
        <div className="w-full max-w-3xl mx-auto p-6">
          <TabsContent value="profile"><AccountTab /></TabsContent>
          <TabsContent value="appearance"><AppearanceTab /></TabsContent>
          <TabsContent value="tokens"><TokensTab /></TabsContent>
          <TabsContent value="workspace"><WorkspaceTab /></TabsContent>
          <TabsContent value="repositories"><RepositoriesTab /></TabsContent>
          <TabsContent value="members"><MembersTab /></TabsContent>
          <TabsContent value="secrets"><SecretsTab /></TabsContent>
        </div>
      </div>
    </Tabs>
  );
}
