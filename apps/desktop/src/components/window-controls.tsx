import type { CSSProperties } from "react";
import { Minus, Square, X } from "lucide-react";

export function WindowControls() {
  return (
    <div
      className="flex items-center gap-1"
      style={{ WebkitAppRegion: "no-drag" } as CSSProperties}
    >
      <button
        type="button"
        onClick={() => void window.myteam.shell.minimizeWindow()}
        className="flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground transition hover:bg-white/5 hover:text-foreground"
        aria-label="Minimize window"
      >
        <Minus className="h-4 w-4" />
      </button>
      <button
        type="button"
        onClick={() => void window.myteam.shell.maximizeWindow()}
        className="flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground transition hover:bg-white/5 hover:text-foreground"
        aria-label="Maximize window"
      >
        <Square className="h-3.5 w-3.5" />
      </button>
      <button
        type="button"
        onClick={() => void window.myteam.shell.closeWindow()}
        className="flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground transition hover:bg-red-500/20 hover:text-red-200"
        aria-label="Close window"
      >
        <X className="h-4 w-4" />
      </button>
    </div>
  );
}
