import { useEffect, useRef, useState } from "react";

const DEFAULT_INITIAL_DELAY_MS = 3000;
const DEFAULT_MAX_DELAY_MS = 30000;

function describePollError(error: unknown, fallback: string): string {
  if (error instanceof Error && error.message) {
    return error.message;
  }
  return fallback;
}

interface UseRetryingPollOptions {
  enabled: boolean;
  poll: () => Promise<void>;
  fallbackError: string;
  resetKey?: string | number;
  initialDelayMs?: number;
  maxDelayMs?: number;
}

export function useRetryingPoll({
  enabled,
  poll,
  fallbackError,
  resetKey,
  initialDelayMs = DEFAULT_INITIAL_DELAY_MS,
  maxDelayMs = DEFAULT_MAX_DELAY_MS,
}: UseRetryingPollOptions) {
  const [error, setError] = useState<string | null>(null);
  const pollRef = useRef(poll);

  useEffect(() => {
    pollRef.current = poll;
  }, [poll]);

  useEffect(() => {
    if (!enabled) {
      setError(null);
      return;
    }

    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | null = null;
    let attempt = 0;

    const clearTimer = () => {
      if (timer) {
        clearTimeout(timer);
        timer = null;
      }
    };

    const scheduleNext = (delayMs: number) => {
      clearTimer();
      timer = setTimeout(() => {
        void run();
      }, delayMs);
    };

    const run = async () => {
      if (cancelled) return;

      try {
        await pollRef.current();
        if (cancelled) return;
        attempt = 0;
        setError(null);
        scheduleNext(initialDelayMs);
      } catch (error) {
        if (cancelled) return;
        attempt += 1;
        setError(describePollError(error, fallbackError));
        const nextDelay = Math.min(
          initialDelayMs * 2 ** Math.max(attempt - 1, 0),
          maxDelayMs
        );
        scheduleNext(nextDelay);
      }
    };

    void run();

    return () => {
      cancelled = true;
      clearTimer();
    };
  }, [enabled, fallbackError, initialDelayMs, maxDelayMs, resetKey]);

  return { error };
}
