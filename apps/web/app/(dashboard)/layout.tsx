"use client";

import { useEffect } from "react";
import { useRouter, usePathname } from "next/navigation";
import { MulticaIcon } from "@/components/multica-icon";
import { useNavigationStore } from "@/features/navigation";
import { SidebarProvider, SidebarInset } from "@/components/ui/sidebar";
import { useAuthStore } from "@/features/auth";
import { useWorkspaceStore } from "@/features/workspace";
import { AppSidebar } from "./_components/app-sidebar";

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
        <MulticaIcon className="size-6" />
      </div>
    );
  }

  if (!user) return null;

  return (
    <SidebarProvider className="h-svh bg-background">
      <AppSidebar />
      <SidebarInset className="overflow-hidden bg-background">
        {workspace ? (
          children
        ) : (
          <div className="flex flex-1 items-center justify-center">
            <MulticaIcon className="size-6 animate-pulse" />
          </div>
        )}
      </SidebarInset>
    </SidebarProvider>
  );
}
