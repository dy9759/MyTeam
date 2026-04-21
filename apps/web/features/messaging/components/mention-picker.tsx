"use client";

import { useEffect, useState } from "react";
import { filterCandidates } from "@myteam/client-core";
import { Bot, User } from "lucide-react";

export interface MentionCandidate {
  id: string;
  name: string;
  kind: "agent" | "owner";
}

interface Props {
  candidates: MentionCandidate[];
  query: string;
  onSelect: (candidate: MentionCandidate) => void;
  onClose: () => void;
}

// MentionPicker renders a floating list of @-mention candidates (members
// and agents) with keyboard navigation. Parent controls visibility via
// the filtered result — if filterCandidates returns empty, the picker
// renders nothing and arrow keys fall through to the textarea.
export function MentionPicker({ candidates, query, onSelect, onClose }: Props) {
  const filtered = filterCandidates(candidates, query).slice(0, 8);
  const [active, setActive] = useState(0);

  useEffect(() => {
    setActive(0);
  }, [query]);

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setActive((i) => (filtered.length === 0 ? 0 : (i + 1) % filtered.length));
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        setActive((i) =>
          filtered.length === 0 ? 0 : (i - 1 + filtered.length) % filtered.length,
        );
      } else if (e.key === "Enter") {
        if (filtered[active]) {
          e.preventDefault();
          onSelect(filtered[active]!);
        }
      } else if (e.key === "Escape") {
        e.preventDefault();
        onClose();
      }
    };
    window.addEventListener("keydown", handler, true);
    return () => window.removeEventListener("keydown", handler, true);
  }, [filtered, active, onSelect, onClose]);

  if (filtered.length === 0) return null;

  return (
    <div className="absolute bottom-full left-0 right-0 z-20 mb-2 overflow-hidden rounded-md border border-border bg-card shadow-lg">
      {filtered.map((c, i) => (
        <button
          key={`${c.kind}:${c.id}`}
          type="button"
          onMouseDown={(e) => {
            e.preventDefault();
            onSelect(c);
          }}
          onMouseEnter={() => setActive(i)}
          className={`flex w-full items-center gap-2 px-3 py-2 text-left text-[13px] ${
            i === active ? "bg-primary text-primary-foreground" : "text-foreground hover:bg-accent"
          }`}
        >
          {c.kind === "agent" ? (
            <Bot className="h-3.5 w-3.5 shrink-0" />
          ) : (
            <User className="h-3.5 w-3.5 shrink-0" />
          )}
          <span className="truncate">{c.name}</span>
        </button>
      ))}
    </div>
  );
}
