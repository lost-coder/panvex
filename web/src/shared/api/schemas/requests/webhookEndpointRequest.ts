import { z } from "zod";

const eventFilterEntry = /^[a-zA-Z0-9._-]+(?:\.\*)?$|^\*$/;

const eventFilterString = z
  .string()
  .max(512)
  .refine(
    (raw) =>
      raw
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean)
        .every((entry) => eventFilterEntry.test(entry)),
    {
      message:
        "event_filter entries must be dot-namespaced actions or 'prefix.*'",
    },
  );

const baseFields = {
  name: z.string().min(1).max(128),
  url: z
    .string()
    .min(1)
    .max(2048)
    .refine((s) => /^https?:\/\//i.test(s.trim()), {
      message: "url must start with http:// or https://",
    }),
  event_filter: eventFilterString,
  allow_private: z.boolean(),
  enabled: z.boolean(),
};

/**
 * Body of POST /api/webhook-endpoints. Secret is required on create
 * (empty would let anyone forge HMAC-signed deliveries).
 */
export const createWebhookEndpointRequestSchema = z.object({
  ...baseFields,
  secret: z.string().min(1).max(1024),
});

/**
 * Body of PUT /api/webhook-endpoints/{id}. Empty Secret means
 * "preserve the existing secret" — see WebhookStore.UpdateEndpoint
 * godoc for the rotate-on-non-empty contract.
 */
export const updateWebhookEndpointRequestSchema = z.object({
  ...baseFields,
  secret: z.string().max(1024),
});

export type CreateWebhookEndpointRequest = z.infer<
  typeof createWebhookEndpointRequestSchema
>;
export type UpdateWebhookEndpointRequest = z.infer<
  typeof updateWebhookEndpointRequestSchema
>;
