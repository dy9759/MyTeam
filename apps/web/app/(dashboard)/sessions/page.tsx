"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { useSessionStore } from "@/features/sessions/store";

const STATUS_COLORS: Record<string, string> = {
  active: "bg-green-100 text-green-700",
  waiting: "bg-yellow-100 text-yellow-700",
  completed: "bg-blue-100 text-blue-700",
  failed: "bg-red-100 text-red-700",
  archived: "bg-gray-100 text-gray-700",
};

export default function SessionsPage() {
  const router = useRouter();
  const { sessions, loading, fetch, createSession } = useSessionStore();
  const [showCreate, setShowCreate] = useState(false);
  const [title, setTitle] = useState("");
  const [maxTurns, setMaxTurns] = useState("");
  const [creating, setCreating] = useState(false);

  useEffect(() => {
    fetch();
  }, [fetch]);

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault();
    if (!title.trim()) return;
    setCreating(true);
    const session = await createSession({
      title: title.trim(),
      max_turns: maxTurns ? parseInt(maxTurns, 10) : undefined,
    });
    setCreating(false);
    if (session) {
      setShowCreate(false);
      setTitle("");
      setMaxTurns("");
      router.push(`/sessions/${session.id}`);
    }
  }

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold">Sessions</h1>
        <button
          onClick={() => setShowCreate(!showCreate)}
          className="px-4 py-2 bg-primary text-primary-foreground rounded-md text-sm font-medium"
        >
          {showCreate ? "Cancel" : "New Session"}
        </button>
      </div>

      {showCreate && (
        <form onSubmit={handleCreate} className="mb-6 p-4 border rounded-lg space-y-3">
          <div>
            <label className="block text-sm font-medium mb-1">Title</label>
            <input
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              className="w-full px-3 py-2 border rounded-md text-sm bg-background"
              placeholder="Bug triage for #42"
              required
            />
          </div>
          <div>
            <label className="block text-sm font-medium mb-1">Max turns (optional)</label>
            <input
              value={maxTurns}
              onChange={(e) => setMaxTurns(e.target.value)}
              type="number"
              min="1"
              className="w-full px-3 py-2 border rounded-md text-sm bg-background"
              placeholder="10"
            />
          </div>
          <button
            type="submit"
            disabled={creating || !title.trim()}
            className="px-4 py-2 bg-primary text-primary-foreground rounded-md text-sm font-medium disabled:opacity-50"
          >
            {creating ? "Creating..." : "Create Session"}
          </button>
        </form>
      )}

      {loading && sessions.length === 0 ? (
        <div className="text-center py-12 text-muted-foreground">Loading...</div>
      ) : sessions.length === 0 ? (
        <div className="text-center py-12">
          <p className="text-muted-foreground mb-2">No sessions yet</p>
          <p className="text-sm text-muted-foreground">
            Create a session to start a multi-turn collaboration with agents about an issue.
          </p>
        </div>
      ) : (
        <div className="space-y-2">
          {sessions.map((s) => (
            <div
              key={s.id}
              onClick={() => router.push(`/sessions/${s.id}`)}
              className="p-4 border rounded-lg hover:bg-muted/50 cursor-pointer"
            >
              <div className="flex items-center justify-between">
                <div className="font-medium">{s.title}</div>
                <span className={`text-xs px-2 py-0.5 rounded ${STATUS_COLORS[s.status] ?? "bg-gray-100"}`}>
                  {s.status}
                </span>
              </div>
              <div className="text-sm text-muted-foreground mt-1">
                Turn {s.current_turn}/{s.max_turns || "\u221E"} · Updated {new Date(s.updated_at).toLocaleString()}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
