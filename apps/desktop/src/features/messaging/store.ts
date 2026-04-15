// Re-export from desktop-client where the messaging store is created,
// to avoid a circular dependency between features/messaging and lib/desktop-client.
export { useDesktopMessagingStore } from "@/lib/desktop-client";
