import { MemoryList } from "@/features/memories/components/memory-list";
import { MemorySearch } from "@/features/memories/components/memory-search";

export default function MemoriesPage() {
  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <header className="flex h-12 shrink-0 items-center border-b border-border px-4">
        <h1 className="text-base font-medium text-foreground">Memories</h1>
      </header>
      <div className="flex min-h-0 flex-1 gap-4 overflow-hidden p-4">
        <MemoryList />
        <MemorySearch />
      </div>
    </div>
  );
}
