"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { useChannelStore } from "@/features/channels/store";

export default function ChannelsPage() {
  const router = useRouter();
  const { channels, loading, fetch, createChannel } = useChannelStore();
  const [showCreate, setShowCreate] = useState(false);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [creating, setCreating] = useState(false);

  useEffect(() => {
    fetch();
  }, [fetch]);

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault();
    if (!name.trim()) return;
    setCreating(true);
    const ch = await createChannel({ name: name.trim(), description: description.trim() || undefined });
    setCreating(false);
    if (ch) {
      setShowCreate(false);
      setName("");
      setDescription("");
      router.push(`/channels/${ch.id}`);
    }
  }

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold">Channels</h1>
        <button
          onClick={() => setShowCreate(!showCreate)}
          className="px-4 py-2 bg-primary text-primary-foreground rounded-md text-sm font-medium"
        >
          {showCreate ? "Cancel" : "Create Channel"}
        </button>
      </div>

      {showCreate && (
        <form onSubmit={handleCreate} className="mb-6 p-4 border rounded-lg space-y-3">
          <div>
            <label className="block text-sm font-medium mb-1">Name</label>
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="w-full px-3 py-2 border rounded-md text-sm bg-background"
              placeholder="general"
              required
            />
          </div>
          <div>
            <label className="block text-sm font-medium mb-1">Description (optional)</label>
            <input
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              className="w-full px-3 py-2 border rounded-md text-sm bg-background"
              placeholder="A channel for general discussion"
            />
          </div>
          <button
            type="submit"
            disabled={creating || !name.trim()}
            className="px-4 py-2 bg-primary text-primary-foreground rounded-md text-sm font-medium disabled:opacity-50"
          >
            {creating ? "Creating..." : "Create"}
          </button>
        </form>
      )}

      {loading && channels.length === 0 ? (
        <div className="text-center py-12 text-muted-foreground">Loading...</div>
      ) : channels.length === 0 ? (
        <div className="text-center py-12">
          <p className="text-muted-foreground mb-2">No channels yet</p>
          <p className="text-sm text-muted-foreground">Create a channel to start group communication with your team and agents.</p>
        </div>
      ) : (
        <div className="space-y-2">
          {channels.map((ch) => (
            <div
              key={ch.id}
              onClick={() => router.push(`/channels/${ch.id}`)}
              className="p-4 border rounded-lg hover:bg-muted/50 cursor-pointer"
            >
              <div className="font-medium">#{ch.name}</div>
              {ch.description && <div className="text-sm text-muted-foreground">{ch.description}</div>}
              <div className="text-xs text-muted-foreground mt-1">
                Created {new Date(ch.created_at).toLocaleDateString()}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
