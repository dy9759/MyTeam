"use client";

import { useCallback, useEffect, useRef } from "react";
import { useWSEvent } from "@/features/realtime";
import { useMemoryStore } from "@/features/memories/store";
import type { MemoryFilter } from "@/features/memories/api";
import type { WSEventType } from "@/shared/types";

const MEMORY_CONFIRMED_EVENT = "memory.confirmed" as WSEventType;
const MEMORY_ARCHIVED_EVENT = "memory.archived" as WSEventType;

export function useMemoryRealtime(filter?: MemoryFilter) {
  const filterRef = useRef(filter);

  useEffect(() => {
    filterRef.current = filter;
  }, [filter]);

  const refreshMemories = useCallback(() => {
    void useMemoryStore.getState().fetchAll(filterRef.current);
  }, []);

  useWSEvent(MEMORY_CONFIRMED_EVENT, refreshMemories);
  useWSEvent(MEMORY_ARCHIVED_EVENT, refreshMemories);
}
