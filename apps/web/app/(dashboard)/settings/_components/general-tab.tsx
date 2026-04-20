"use client";

import { useTheme } from "next-themes";
import { cn } from "@/lib/utils";

function WindowMockup({
  variant,
  className,
}: {
  variant: "light" | "dark";
  className?: string;
}) {
  return (
    <div className={cn("flex h-full w-full flex-col overflow-hidden", variant, className)}>
      {/* Title bar */}
      <div className="flex items-center gap-[3px] bg-muted px-2 py-1.5">
        <span className="size-[6px] rounded-full bg-[#ff5f57]" />
        <span className="size-[6px] rounded-full bg-[#febc2e]" />
        <span className="size-[6px] rounded-full bg-[#28c840]" />
      </div>
      {/* Content area */}
      <div className="flex flex-1 bg-background">
        {/* Sidebar */}
        <div className="w-[30%] space-y-1 bg-secondary p-2">
          <div className="h-1 w-3/4 rounded-full bg-muted-foreground/30" />
          <div className="h-1 w-1/2 rounded-full bg-muted-foreground/30" />
        </div>
        {/* Main */}
        <div className="flex-1 space-y-1.5 p-2">
          <div className="h-1.5 w-4/5 rounded-full bg-muted-foreground/35" />
          <div className="h-1 w-full rounded-full bg-muted-foreground/25" />
          <div className="h-1 w-3/5 rounded-full bg-muted-foreground/25" />
        </div>
      </div>
    </div>
  );
}

const themeOptions = [
  { value: "light" as const, label: "浅色" },
  { value: "dark" as const, label: "深色" },
  { value: "system" as const, label: "跟随系统" },
];

export function AppearanceTab() {
  const { theme, setTheme } = useTheme();

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">主题</h2>
        <div className="flex gap-6" role="radiogroup" aria-label="主题">
          {themeOptions.map((opt) => {
            const active = theme === opt.value;
            return (
              <button
                key={opt.value}
                role="radio"
                aria-checked={active}
                aria-label={`Select ${opt.label} theme`}
                onClick={() => setTheme(opt.value)}
                className="group flex flex-col items-center gap-2"
              >
                <div
                  className={cn(
                    "aspect-[4/3] w-36 overflow-hidden rounded-lg ring-1 transition-all",
                    active
                      ? "ring-2 ring-brand"
                      : "ring-border hover:ring-2 hover:ring-border"
                  )}
                >
                  {opt.value === "system" ? (
                    <div className="relative h-full w-full">
                      <WindowMockup
                        variant="light"
                        className="absolute inset-0"
                      />
                      <WindowMockup
                        variant="dark"
                        className="absolute inset-0 [clip-path:inset(0_0_0_50%)]"
                      />
                    </div>
                  ) : (
                    <WindowMockup variant={opt.value} />
                  )}
                </div>
                <span
                  className={cn(
                    "text-sm transition-colors",
                    active
                      ? "font-medium text-foreground"
                      : "text-muted-foreground"
                  )}
                >
                  {opt.label}
                </span>
              </button>
            );
          })}
        </div>
      </section>
    </div>
  );
}
