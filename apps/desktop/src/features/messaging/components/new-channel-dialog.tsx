import { useState } from "react";

interface Props {
  onCreate: (name: string) => Promise<void> | void;
  onClose: () => void;
}

export function NewChannelDialog({ onCreate, onClose }: Props) {
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    if (!name.trim() || busy) return;
    setBusy(true);
    try {
      await onCreate(name.trim());
    } finally {
      setBusy(false);
    }
  };

  return (
    <div
      className="fixed inset-0 z-30 flex items-center justify-center bg-black/40"
      onClick={onClose}
    >
      <div
        className="w-full max-w-md rounded-[28px] border border-border/70 bg-card/95 p-6 shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <h2 className="text-lg font-medium text-foreground">New channel</h2>
        <input
          autoFocus
          placeholder="Channel name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="mt-4 w-full rounded-2xl border border-border/70 bg-background/70 px-4 py-2 text-sm outline-none focus:border-primary"
        />
        <p className="mt-2 text-xs text-muted-foreground">
          Channel will be private. You can invite members later.
        </p>
        <div className="mt-6 flex justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            className="rounded-2xl border border-border/70 px-4 py-2 text-sm text-foreground"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={submit}
            disabled={!name.trim() || busy}
            className="rounded-2xl bg-primary px-4 py-2 text-sm font-medium text-primary-foreground disabled:opacity-50"
          >
            Create
          </button>
        </div>
      </div>
    </div>
  );
}
