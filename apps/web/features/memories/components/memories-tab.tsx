"use client";

import { MemoryList } from "./memory-list";
import { MemorySearch } from "./memory-search";
import { useMemoryRealtime } from "@/features/memories";

// MemoriesTab is the "memories as summarized wiki" surface embedded
// inside the /files page. It's intentionally a thin wrapper so the
// stand-alone /memories route can also render it during the transition
// period.
export function MemoriesTab() {
  useMemoryRealtime();

  return (
    <div className="flex min-h-0 flex-1 gap-4 overflow-hidden">
      <MemoryList />
      <MemorySearch />
    </div>
  );
}
