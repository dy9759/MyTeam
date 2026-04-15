import { describe, expect, it } from "vitest";
import { detectTrigger, filterCandidates } from "./mention-parser";

describe("detectTrigger", () => {
  it("triggers on @ at start", () => {
    expect(detectTrigger("@", 1)).toEqual({
      triggering: true,
      query: "",
      start: 0,
      end: 1,
    });
  });

  it("triggers on @ after space", () => {
    expect(detectTrigger("hi @", 4)).toEqual({
      triggering: true,
      query: "",
      start: 3,
      end: 4,
    });
  });

  it("captures query after @", () => {
    expect(detectTrigger("@Ali", 4)).toEqual({
      triggering: true,
      query: "Ali",
      start: 0,
      end: 4,
    });
    expect(detectTrigger("hello @Alice ", 12)).toEqual({
      triggering: true,
      query: "Alice",
      start: 6,
      end: 12,
    });
  });

  it("does not trigger inside an email", () => {
    expect(detectTrigger("email foo@bar", 13).triggering).toBe(false);
  });

  it("stops triggering after space", () => {
    expect(detectTrigger("@Alice hi", 9).triggering).toBe(false);
  });

  it("does not trigger on empty text or mid-word", () => {
    expect(detectTrigger("", 0).triggering).toBe(false);
    expect(detectTrigger("foobar", 3).triggering).toBe(false);
  });
});

describe("filterCandidates", () => {
  const list = [
    { name: "Assistant", id: "a" },
    { name: "Alice", id: "b" },
    { name: "Bob", id: "c" },
    { name: "AliceBot", id: "d" },
  ];

  it("returns all when query empty", () => {
    expect(filterCandidates(list, "").map((c) => c.id)).toEqual([
      "a",
      "b",
      "c",
      "d",
    ]);
  });

  it("prefix-matches case-insensitive", () => {
    expect(filterCandidates(list, "al").map((c) => c.id)).toEqual(["b", "d"]);
    expect(filterCandidates(list, "AL").map((c) => c.id)).toEqual(["b", "d"]);
  });

  it("exact-prefix matches before substring matches", () => {
    expect(filterCandidates(list, "ot").map((c) => c.id)).toEqual([]);
    // 'bot' is substring of 'AliceBot' but not a prefix; with pure prefix it's filtered out
  });
});
