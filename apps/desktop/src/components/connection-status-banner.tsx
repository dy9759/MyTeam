import { useWSStatusStore } from "@/lib/desktop-client";

export function ConnectionStatusBanner() {
  const status = useWSStatusStore((s) => s.status);
  if (status === "connected") return null;

  const text =
    status === "reconnecting"
      ? "连接中断,正在重连..."
      : status === "connecting"
      ? "正在连接服务器..."
      : "连接失败,请检查网络";

  const color =
    status === "reconnecting" || status === "connecting"
      ? "bg-amber-500/20 text-amber-200"
      : "bg-destructive/20 text-destructive";

  return (
    <div
      className={`flex h-8 items-center justify-center text-xs font-medium ${color}`}
    >
      {text}
    </div>
  );
}
