"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import {
  ChevronDown,
  Settings,
  LogOut,
  User,
  Plus,
  Check,
  SquarePen,
  MessageSquare,
  FolderGit2,
  FileText,
} from "lucide-react";
import { WorkspaceAvatar } from "@/features/workspace";
import { useIssueDraftStore } from "@/features/issues/stores/draft-store";
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarHeader,
  SidebarFooter,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
} from "@/components/ui/sidebar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Tooltip, TooltipTrigger, TooltipContent } from "@/components/ui/tooltip";
import { useAuthStore } from "@/features/auth";
import { useWorkspaceStore } from "@/features/workspace";
import { useModalStore } from "@/features/modals";

const navItems = [
  { href: "/session", label: "会话", icon: MessageSquare },
  { href: "/projects", label: "项目", icon: FolderGit2 },
  { href: "/files", label: "文件 + 记忆", icon: FileText },
  { href: "/account", label: "身份", icon: User },
  { href: "/settings", label: "设置", icon: Settings },
];

function DraftDot() {
  const hasDraft = useIssueDraftStore((s) => !!(s.draft.title || s.draft.description));
  if (!hasDraft) return null;
  return <span className="absolute top-0 right-0 size-1.5 rounded-full bg-brand" />;
}

export function AppSidebar() {
  const pathname = usePathname();
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const authLogout = useAuthStore((s) => s.logout);
  const workspace = useWorkspaceStore((s) => s.workspace);
  const workspaces = useWorkspaceStore((s) => s.workspaces);
  const switchWorkspace = useWorkspaceStore((s) => s.switchWorkspace);

  const logout = () => {
    router.push("/");
    authLogout();
    useWorkspaceStore.getState().clearWorkspace();
  };

  return (
      <Sidebar variant="inset" collapsible="icon" className="bg-card border-r border-border">
        {/* Workspace Switcher */}
        <SidebarHeader className="py-3">
          <div className="flex items-center gap-4">
            <SidebarMenu className="min-w-0 flex-1">
              <SidebarMenuItem>
                <DropdownMenu>
                  <DropdownMenuTrigger
                    render={
                      <SidebarMenuButton>
                        <WorkspaceAvatar name={workspace?.name ?? "M"} size="sm" />
                        <span className="flex-1 truncate font-medium text-foreground">
                          {workspace?.name ?? "My Team"}
                        </span>
                        <ChevronDown className="size-3 text-muted-foreground" />
                      </SidebarMenuButton>
                    }
                  />
                <DropdownMenuContent
                  className="w-52 bg-popover border-border"
                  align="start"
                  side="bottom"
                  sideOffset={4}
                >
                  <DropdownMenuGroup>
                    <DropdownMenuLabel className="text-xs text-muted-foreground">
                      {user?.email}
                    </DropdownMenuLabel>
                  </DropdownMenuGroup>
                  <DropdownMenuSeparator />
                  <DropdownMenuGroup className="group/ws-section">
                    <DropdownMenuLabel className="flex items-center text-xs text-muted-foreground">
                      工作区
                      <Tooltip>
                        <TooltipTrigger
                          className="ml-auto opacity-0 group-hover/ws-section:opacity-100 transition-opacity rounded hover:bg-accent p-0.5"
                          onClick={() => useModalStore.getState().open("create-workspace")}
                        >
                          <Plus className="h-3.5 w-3.5" />
                        </TooltipTrigger>
                        <TooltipContent side="right">
                          创建工作区
                        </TooltipContent>
                      </Tooltip>
                    </DropdownMenuLabel>
                    {workspaces.map((ws) => (
                      <DropdownMenuItem
                        key={ws.id}
                        onClick={() => {
                          if (ws.id !== workspace?.id) {
                            switchWorkspace(ws.id);
                          }
                        }}
                      >
                        <WorkspaceAvatar name={ws.name} size="sm" />
                        <span className="flex-1 truncate">{ws.name}</span>
                        {ws.id === workspace?.id && (
                          <Check className="h-3.5 w-3.5 text-primary" />
                        )}
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuGroup>
                  <DropdownMenuSeparator />
                  <DropdownMenuGroup>
                    <DropdownMenuItem variant="destructive" onClick={logout}>
                      <LogOut className="h-3.5 w-3.5" />
                      退出登录
                    </DropdownMenuItem>
                  </DropdownMenuGroup>
                </DropdownMenuContent>
                </DropdownMenu>
              </SidebarMenuItem>
            </SidebarMenu>
            <Tooltip>
              <TooltipTrigger
                className="relative flex h-7 w-7 items-center justify-center rounded-lg bg-accent text-secondary-foreground hover:bg-muted hover:text-foreground"
                onClick={() => useModalStore.getState().open("create-issue")}
              >
                <SquarePen className="size-3.5" />
                <DraftDot />
              </TooltipTrigger>
              <TooltipContent side="bottom">新建任务</TooltipContent>
            </Tooltip>
          </div>
        </SidebarHeader>

        {/* Navigation — 5 core pages */}
        <SidebarContent>
          <SidebarGroup>
            <SidebarGroupContent>
              <SidebarMenu className="gap-0.5">
                {navItems.map((item) => {
                  const isActive = pathname === item.href || pathname.startsWith(item.href + "/");
                  return (
                    <SidebarMenuItem key={item.href}>
                      <SidebarMenuButton
                        isActive={isActive}
                        render={<Link href={item.href} />}
                        className="text-secondary-foreground hover:text-foreground hover:not-data-active:bg-accent/50 data-active:bg-accent data-active:text-foreground"
                      >
                        <item.icon />
                        <span>{item.label}</span>
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                  );
                })}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        </SidebarContent>
        <SidebarFooter />
        <SidebarRail />
      </Sidebar>
  );
}
