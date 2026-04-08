"use client";

import { UserCheck } from "lucide-react";

interface ImpersonationIndicatorProps {
  ownerName?: string;
}

export function ImpersonationIndicator({ ownerName }: ImpersonationIndicatorProps) {
  return (
    <span className="inline-flex items-center gap-0.5 text-xs text-muted-foreground ml-1">
      <UserCheck className="h-3 w-3" />
      <span>via {ownerName ?? "Owner"}</span>
    </span>
  );
}
