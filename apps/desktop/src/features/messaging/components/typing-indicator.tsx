interface Props {
  agentName: string;
}

export function TypingIndicator({ agentName }: Props) {
  return (
    <div className="flex items-center gap-2 rounded-3xl border border-border/70 bg-background/70 px-4 py-3 text-sm text-muted-foreground">
      <span className="inline-flex gap-1">
        <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-muted-foreground [animation-delay:0ms]" />
        <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-muted-foreground [animation-delay:150ms]" />
        <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-muted-foreground [animation-delay:300ms]" />
      </span>
      <span>{agentName} is typing...</span>
    </div>
  );
}
