import { createMessagingStore, type MessagingApiClient } from "@myteam/client-core";
import { desktopApi } from "@/lib/desktop-client";

const apiAdapter: MessagingApiClient = {
  listConversations: () => desktopApi.listConversations(),
  listChannels: () => desktopApi.listChannels(),
  listMessages: (params) => desktopApi.listMessages(params),
  sendMessage: (params) => desktopApi.sendMessage(params),
  createChannel: (params) => desktopApi.createChannel(params),
};

export const useDesktopMessagingStore = createMessagingStore({
  apiClient: apiAdapter,
  onError: (msg) => {
    // MVP: surface via console. Replace with toast component later.
    // eslint-disable-next-line no-console
    console.error("[messaging]", msg);
  },
});
