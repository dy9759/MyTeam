import { useState } from "react";
import { filterCandidates } from "@myteam/client-core";

export interface DMCandidate {
  id: string;
  name: string;
  kind: "agent" | "owner";
}

interface Props {
  candidates: DMCandidate[];
  onSelect: (peerId: string, peerType: "agent" | "member") => void;
  onClose: () => void;
}

export function NewDMDialog({ candidates, onSelect, onClose }: Props) {
  const [query, setQuery] = useState("");
  const filtered = filterCandidates(candidates, query);
  return (
    <div
      className="fixed inset-0 z-30 flex items-center justify-center bg-black/40"
      onClick={onClose}
    >
      <div
        className="w-full max-w-md rounded-[28px] border border-border/70 bg-card/95 p-6 shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <h2 className="text-lg font-medium text-foreground">Start a new DM</h2>
        <input
          autoFocus
          placeholder="Search agents and members"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          className="mt-4 w-full rounded-2xl border border-border/70 bg-background/70 px-4 py-2 text-sm outline-none focus:border-primary"
        />
        <div className="mt-4 max-h-80 overflow-y-auto">
          {filtered.length === 0 ? (
            <p className="px-4 py-8 text-center text-sm text-muted-foreground">
              No matches.
            </p>
          ) : (
            filtered.map((c) => (
              <button
                key={c.id}
                type="button"
                onClick={() =>
                  onSelect(c.id, c.kind === "agent" ? "agent" : "member")
                }
                className="flex w-full items-center gap-3 rounded-2xl px-4 py-3 text-left text-sm hover:bg-white/5"
              >
                <span>{c.kind === "agent" ? "🤖" : "👤"}</span>
                <span className="truncate text-foreground">{c.name}</span>
              </button>
            ))
          )}
        </div>
      </div>
    </div>
  );
}
