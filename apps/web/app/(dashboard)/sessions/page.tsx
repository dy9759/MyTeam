"use client";
import { useState } from "react";

const STATUS_COLORS: Record<string, string> = {
  active: "bg-green-100 text-green-700",
  waiting: "bg-yellow-100 text-yellow-700",
  completed: "bg-blue-100 text-blue-700",
  failed: "bg-red-100 text-red-700",
  archived: "bg-gray-100 text-gray-700",
};

export default function SessionsPage() {
  const [sessions] = useState<Array<{
    id: string;
    title: string;
    status: string;
    current_turn: number;
    max_turns: number;
    updated_at: string;
  }>>([]);

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold">Sessions</h1>
        <button className="px-4 py-2 bg-primary text-primary-foreground rounded-md text-sm font-medium">
          New Session
        </button>
      </div>

      {sessions.length === 0 ? (
        <div className="text-center py-12">
          <p className="text-muted-foreground mb-2">No sessions yet</p>
          <p className="text-sm text-muted-foreground">
            Create a session to start a multi-turn collaboration with agents about an issue.
          </p>
        </div>
      ) : (
        <div className="space-y-2">
          {sessions.map(s => (
            <div key={s.id} className="p-4 border rounded-lg hover:bg-muted/50 cursor-pointer">
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
