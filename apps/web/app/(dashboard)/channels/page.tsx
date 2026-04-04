"use client";
import { useState } from "react";

export default function ChannelsPage() {
  const [channels] = useState<Array<{ id: string; name: string; description?: string }>>([]);

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold">Channels</h1>
        <button className="px-4 py-2 bg-primary text-primary-foreground rounded-md text-sm font-medium">
          Create Channel
        </button>
      </div>

      {channels.length === 0 ? (
        <div className="text-center py-12">
          <p className="text-muted-foreground mb-2">No channels yet</p>
          <p className="text-sm text-muted-foreground">Create a channel to start group communication with your team and agents.</p>
        </div>
      ) : (
        <div className="space-y-2">
          {channels.map(ch => (
            <div key={ch.id} className="p-4 border rounded-lg hover:bg-muted/50 cursor-pointer">
              <div className="font-medium">#{ch.name}</div>
              {ch.description && <div className="text-sm text-muted-foreground">{ch.description}</div>}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
