"use client";

import { toast } from "sonner";
import { api } from "@/shared/api";
import {
  createMessagingStore,
  type MessagingApiClient,
} from "@myteam/client-core";

// Adapter: map the web `api` singleton to the factory's contract.
const apiAdapter: MessagingApiClient = {
  listConversations: () => api.listConversations(),
  listChannels: () => api.listChannels(),
  listMessages: (params) => api.listMessages(params),
  sendMessage: (params) => api.sendMessage(params),
  createChannel: (params) => api.createChannel(params),
};

export const useMessagingStore = createMessagingStore({
  apiClient: apiAdapter,
  onError: (msg) => toast.error(msg),
});
