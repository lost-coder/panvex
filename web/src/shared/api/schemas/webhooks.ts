import { z } from "zod";

import { id } from "./common.ts";

/**
 * Response schema for /api/webhook-endpoints — admin-only outbox
 * receiver inventory. Mirrors webhookEndpointDTO in
 * internal/controlplane/server/http_webhooks.go: secret never crosses
 * the wire (encrypted at rest, only the worker decrypts), event_filter
 * is shipped as a comma-separated string for form ergonomics.
 */
export const webhookEndpointSchema = z.object({
  id,
  name: z.string(),
  url: z.string(),
  event_filter: z.string(),
  allow_private: z.boolean(),
  enabled: z.boolean(),
});

export const webhookEndpointListResponseSchema = z.object({
  endpoints: z.array(webhookEndpointSchema),
});

export type WebhookEndpointParsed = z.infer<typeof webhookEndpointSchema>;
