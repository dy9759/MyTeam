"use client";

import { useState, type FormEvent } from "react";
import { api } from "@/shared/api";
import type { ParticipantSlot, TaskStatus } from "@/shared/types";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";

interface SlotInputFormProps {
  slot: ParticipantSlot;
  onSubmit: (slot: ParticipantSlot, taskStatus?: TaskStatus) => void;
}

export function SlotInputForm({ slot, onSubmit }: SlotInputFormProps) {
  const [content, setContent] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const value = content.trim();
    if (!value || submitting) return;

    setSubmitting(true);
    setError(null);
    const optimisticSlot: ParticipantSlot = {
      ...slot,
      status: "submitted",
      content: value,
      completed_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    };
    onSubmit(optimisticSlot);

    try {
      const result = await api.submitSlotInput(slot.id, value);
      onSubmit(result.slot, result.task_new_status);
      setContent("");
    } catch (e) {
      onSubmit(slot);
      setError(e instanceof Error ? e.message : "Failed to submit input");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <form className="mt-3 flex flex-col gap-2" onSubmit={handleSubmit}>
      <Textarea
        value={content}
        onChange={(event) => setContent(event.target.value)}
        placeholder="Add your input"
        rows={4}
        disabled={submitting}
      />
      {error && <p className="text-destructive text-xs">{error}</p>}
      <div className="flex justify-end">
        <Button type="submit" size="sm" disabled={submitting || !content.trim()}>
          {submitting ? "Submitting..." : "Submit input"}
        </Button>
      </div>
    </form>
  );
}
