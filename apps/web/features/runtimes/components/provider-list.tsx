"use client";

import { useEffect, useState } from "react";
import { api } from "@/shared/api";
import type { Provider } from "@/shared/types/provider";

export function ProviderList() {
  const [providers, setProviders] = useState<Provider[]>([]);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api.listProviders()
      .then(setProviders)
      .catch((e: unknown) => setError(e instanceof Error ? e.message : "Failed"));
  }, []);

  if (error) {
    return <p className="text-destructive text-sm">{error}</p>;
  }
  if (providers.length === 0) {
    return <p className="text-muted-foreground text-sm">Loading providers…</p>;
  }
  return (
    <ul className="divide-y rounded-md border">
      {providers.map((p) => (
        <li key={p.key} className="flex items-center justify-between p-3">
          <div>
            <div className="font-medium">{p.display_name}</div>
            <div className="text-muted-foreground text-xs">
              {p.kind === "local_cli" ? `CLI: ${p.executable}` : "Cloud API"}
            </div>
          </div>
          {p.default_model && (
            <span className="text-muted-foreground text-xs">{p.default_model}</span>
          )}
        </li>
      ))}
    </ul>
  );
}
