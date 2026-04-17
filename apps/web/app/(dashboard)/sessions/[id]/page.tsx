import { redirect } from "next/navigation";

// TODO: once a server action is wired up, look up the legacy session ID in
// session_migration_map and redirect to the specific channel/thread.
// For now, funnel all legacy /sessions/* links to the new /session index.
export default function SessionDetailRedirect() {
  redirect("/session");
}
