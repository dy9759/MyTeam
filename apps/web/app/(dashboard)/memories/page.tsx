import { redirect } from "next/navigation";

// Memories now lives as a tab inside /files so files (RAW) and memories
// (vector-summarized wiki derived from them) share one workspace surface.
// The old /memories URL redirects to keep bookmarks working.
export default function Page() {
  redirect("/files?tab=memories");
}
