"use client";

import { useEffect } from "react";
import { useRouter, usePathname } from "next/navigation";
import { MyTeamIcon } from "@/components/myteam-icon";
import { useNavigationStore } from "@/features/navigation";
import { SidebarProvider, SidebarInset, SidebarTrigger } from "@/components/ui/sidebar";
import { useAuthStore } from "@/features/auth";
import { useWorkspaceStore } from "@/features/workspace";
import { AppSidebar } from "./_components/app-sidebar";
import { CommandSearch } from "@/features/search";

export default function DashboardLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const workspace = useWorkspaceStore((s) => s.workspace);

  useEffect(() => {
    if (!isLoading && !user) {
      router.push("/");
    }
  }, [user, isLoading, router]);

  useEffect(() => {
    useNavigationStore.getState().onPathChange(pathname);
  }, [pathname]);

  if (isLoading) {
    return (
      <div className="flex h-screen items-center justify-center bg-background">
        <MyTeamIcon className="size-6" />
      </div>
    );
  }

  if (!user) return null;

  return (
    <SidebarProvider className="h-svh bg-background">
      <AppSidebar />
      <SidebarInset className="overflow-hidden bg-background">
        <SidebarTrigger className="fixed top-3 left-3 z-20 md:hidden" />
        {workspace ? (
          children
        ) : (
          <div className="flex flex-1 items-center justify-center">
            <MyTeamIcon className="size-6 animate-pulse" />
          </div>
        )}
      </SidebarInset>
      <CommandSearch />
    </SidebarProvider>
  );
}
