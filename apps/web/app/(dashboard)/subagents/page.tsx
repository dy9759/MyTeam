import { redirect } from "next/navigation";

// Subagents now live as a tab under /account so users manage their
// identity pieces (agents, skills, subagents) in one place. Keep the
// top-level route alive as a redirect so old bookmarks survive.
export default function Page() {
  redirect("/account?tab=subagents");
}
