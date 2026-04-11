export interface SearchResult {
  type: "message" | "issue" | "agent" | "file";
  id: string;
  title: string;
  preview: string;
  score: number;
}

export interface SearchResponse {
  results: SearchResult[];
  total: number;
}
