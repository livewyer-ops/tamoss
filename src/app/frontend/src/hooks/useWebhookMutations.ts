import { useCallback, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { TamossApiClient } from "@/api/client";
import { apiQueryKeys } from "@/api/query";
import type { WebhookDetail, WebhookWritePayload } from "@/types/tams";

export type RefreshCallback = () => void | Promise<void>;

interface WebhookMutationOptions {
  api: TamossApiClient;
  refreshWebhooks: RefreshCallback;
  refreshWebhookDetail?: RefreshCallback;
  onCreated?: (webhook: WebhookDetail) => void;
  onUpdated?: (webhookId: string, webhook: WebhookDetail) => void;
  onDeleted?: (webhookId: string) => void;
}

async function refreshAfterMutation(
  invalidateApiQueries: () => Promise<void>,
  ...callbacks: RefreshCallback[]
): Promise<void> {
  await invalidateApiQueries();
  for (const callback of callbacks) {
    await callback();
  }
}

export function useWebhookMutations({
  api,
  refreshWebhooks,
  refreshWebhookDetail,
  onCreated,
  onUpdated,
  onDeleted,
}: WebhookMutationOptions) {
  const [busy, setBusy] = useState(false);
  const queryClient = useQueryClient();
  const invalidateApiQueries = useCallback(
    () => queryClient.invalidateQueries({ queryKey: apiQueryKeys.all }),
    [queryClient],
  );

  const createWebhook = useCallback(
    async (payload: WebhookWritePayload) => {
      setBusy(true);
      try {
        const created = await api.createWebhook(payload);
        onCreated?.(created);
        await refreshAfterMutation(invalidateApiQueries, refreshWebhooks);
      } finally {
        setBusy(false);
      }
    },
    [api, invalidateApiQueries, refreshWebhooks, onCreated],
  );

  const updateWebhook = useCallback(
    async (webhookId: string, payload: WebhookWritePayload) => {
      setBusy(true);
      try {
        const updated = await api.updateWebhook(webhookId, payload);
        onUpdated?.(webhookId, updated);
        await refreshAfterMutation(
          invalidateApiQueries,
          refreshWebhooks,
          ...(refreshWebhookDetail ? [refreshWebhookDetail] : []),
        );
      } finally {
        setBusy(false);
      }
    },
    [
      api,
      invalidateApiQueries,
      refreshWebhookDetail,
      refreshWebhooks,
      onUpdated,
    ],
  );

  const deleteWebhook = useCallback(
    async (webhookId: string) => {
      setBusy(true);
      try {
        await api.deleteWebhook(webhookId);
        onDeleted?.(webhookId);
        await refreshAfterMutation(invalidateApiQueries, refreshWebhooks);
      } finally {
        setBusy(false);
      }
    },
    [api, invalidateApiQueries, refreshWebhooks, onDeleted],
  );

  return {
    busy,
    createWebhook,
    updateWebhook,
    deleteWebhook,
  };
}
