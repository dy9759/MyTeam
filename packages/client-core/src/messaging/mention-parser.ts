export interface MentionTrigger {
  triggering: boolean;
  query: string;
  start: number; // position of @
  end: number;   // caret position
}

/**
 * Detect whether the caret position in `text` is inside a mention token.
 * A mention token starts with `@` that is preceded by start-of-text or
 * whitespace, and extends until the next whitespace.
 */
export function detectTrigger(text: string, caret: number): MentionTrigger {
  const empty: MentionTrigger = { triggering: false, query: "", start: -1, end: caret };
  if (caret <= 0 || caret > text.length) return empty;

  // Walk backwards from caret to find `@`, stopping at whitespace.
  let i = caret - 1;
  while (i >= 0) {
    const ch = text.charAt(i);
    if (ch === "@") break;
    if (/\s/.test(ch)) return empty;
    i--;
  }
  if (i < 0 || text.charAt(i) !== "@") return empty;

  // The char before `@` must be start-of-text or whitespace.
  if (i > 0) {
    const charBefore = text.charAt(i - 1);
    if (charBefore && !/\s/.test(charBefore)) return empty;
  }

  return {
    triggering: true,
    query: text.slice(i + 1, caret),
    start: i,
    end: caret,
  };
}

/**
 * Case-insensitive prefix match. Preserves list order among matches.
 */
export function filterCandidates<T extends { name: string }>(
  list: T[],
  query: string,
): T[] {
  if (!query) return list;
  const q = query.toLowerCase();
  return list.filter((item) => item.name.toLowerCase().startsWith(q));
}
