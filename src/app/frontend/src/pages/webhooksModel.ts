import type { WebhookDetail, WebhookEvent } from "@/types/tams";

export const statusVariants = {
  created: "info",
  started: "success",
  disabled: "warning",
  error: "danger",
} as const;

export const supportedEvents = [
  "flows/created",
  "flows/updated",
  "flows/deleted",
  "flows/segments_added",
  "flows/segments_deleted",
  "sources/created",
  "sources/updated",
  "sources/deleted",
] as const satisfies readonly WebhookEvent[];

export const WEBHOOK_PAGE_SIZE = "50";

export function buildWebhookLifecycle(webhook: WebhookDetail) {
  return [
    {
      id: "registered",
      label: "Webhook registered",
      description:
        "The webhook configuration exists in TAMOSS and can be inspected or updated.",
      state: webhook.id ? "complete" : "pending",
      timestamp: undefined,
    },
    {
      id: "delivery",
      label: "Delivery active",
      description:
        "The webhook is available to receive matching outbound events from the service.",
      state:
        webhook.status === "started"
          ? "active"
          : webhook.status === "disabled"
            ? "pending"
            : webhook.status === "error"
              ? "error"
              : webhook.status === "created"
                ? "complete"
                : "pending",
      timestamp: webhook.error?.time,
    },
    {
      id: "state",
      label:
        webhook.status === "disabled"
          ? "Webhook disabled"
          : webhook.status === "error"
            ? "Webhook error"
            : "Webhook operational state",
      description:
        webhook.status === "error"
          ? (webhook.error?.summary ??
            "The webhook entered an error state and needs operator attention.")
          : webhook.status === "disabled"
            ? "The webhook is currently disabled and will not receive new events."
            : "The webhook is ready for normal delivery behavior.",
      state:
        webhook.status === "error"
          ? "error"
          : webhook.status === "disabled"
            ? "complete"
            : webhook.status === "started"
              ? "complete"
              : webhook.status === "created"
                ? "active"
                : "pending",
      timestamp: webhook.error?.time,
    },
  ] as const;
}

export function parseCsv(value: string): string[] | undefined {
  const parsed = value
    .split(",")
    .map((entry) => entry.trim())
    .filter(Boolean);
  return parsed.length ? parsed : undefined;
}
