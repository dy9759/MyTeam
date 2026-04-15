import { useEffect, useState } from "react";
import { filterCandidates } from "@myteam/client-core";

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
    <div className="absolute bottom-full left-0 right-0 z-20 mb-2 overflow-hidden rounded-2xl border border-border/70 bg-card/95 shadow-lg backdrop-blur">
      {filtered.map((c, i) => (
        <button
          key={c.id}
          type="button"
          onMouseDown={(e) => {
            e.preventDefault();
            onSelect(c);
          }}
          onMouseEnter={() => setActive(i)}
          className={`flex w-full items-center gap-3 px-4 py-2 text-left text-sm ${
            i === active
              ? "bg-primary text-primary-foreground"
              : "text-foreground hover:bg-white/5"
          }`}
        >
          <span>{c.kind === "agent" ? "🤖" : "👤"}</span>
          <span className="truncate">{c.name}</span>
        </button>
      ))}
    </div>
  );
}
