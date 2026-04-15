import { useRef, useState } from "react";
import { detectTrigger, filterCandidates } from "@myteam/client-core";
import { MentionPicker, type MentionCandidate } from "./mention-picker";

interface Props {
  placeholder: string;
  candidates: MentionCandidate[];
  onSend: (text: string) => Promise<void> | void;
  sending?: boolean;
}

export function MessageInput({ placeholder, candidates, onSend, sending }: Props) {
  const [value, setValue] = useState("");
  const [pickerOpen, setPickerOpen] = useState(false);
  const [pickerQuery, setPickerQuery] = useState("");
  const [pickerRange, setPickerRange] = useState<{ start: number; end: number }>({
    start: 0,
    end: 0,
  });
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const recomputeTrigger = (text: string, pos: number) => {
    const t = detectTrigger(text, pos);
    // Only "open" the picker when there's at least one match — otherwise the
    // parent's pickerOpen diverges from the actual picker UI and Enter gets
    // swallowed without sending.
    const hasMatches =
      t.triggering && filterCandidates(candidates, t.query).length > 0;
    setPickerOpen(hasMatches);
    setPickerQuery(t.query);
    if (t.triggering) setPickerRange({ start: t.start, end: t.end });
  };

  const handleChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    const next = e.target.value;
    const pos = e.target.selectionStart ?? next.length;
    setValue(next);
    recomputeTrigger(next, pos);
  };

  const handleSelect = (e: React.SyntheticEvent<HTMLTextAreaElement>) => {
    const target = e.currentTarget;
    const pos = target.selectionStart ?? value.length;
    recomputeTrigger(value, pos);
  };

  const submit = async () => {
    const text = value.trim();
    if (!text || sending) return;
    await onSend(text);
    setValue("");
    setPickerOpen(false);
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (pickerOpen && ["ArrowUp", "ArrowDown", "Enter", "Escape"].includes(e.key)) {
      return;
    }
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      void submit();
    }
  };

  const insertMention = (c: MentionCandidate) => {
    const before = value.slice(0, pickerRange.start);
    const after = value.slice(pickerRange.end);
    const inserted = `@${c.name} `;
    const next = before + inserted + after;
    setValue(next);
    setPickerOpen(false);
    const newPos = (before + inserted).length;
    requestAnimationFrame(() => {
      textareaRef.current?.focus();
      textareaRef.current?.setSelectionRange(newPos, newPos);
    });
  };

  return (
    <div className="relative">
      {pickerOpen ? (
        <MentionPicker
          candidates={candidates}
          query={pickerQuery}
          onSelect={insertMention}
          onClose={() => setPickerOpen(false)}
        />
      ) : null}
      <textarea
        ref={textareaRef}
        value={value}
        onChange={handleChange}
        onSelect={handleSelect}
        onKeyDown={handleKeyDown}
        disabled={sending}
        placeholder={placeholder}
        rows={3}
        className="w-full resize-none rounded-3xl border border-border/70 bg-background/70 px-4 py-3 text-sm text-foreground outline-none focus:border-primary disabled:opacity-50"
      />
    </div>
  );
}
