import { api, apiBasePath, encodeRequest } from "./http";
import {
  createWebhookEndpointRequestSchema,
  updateWebhookEndpointRequestSchema,
  webhookEndpointListResponseSchema,
  webhookEndpointSchema,
  type CreateWebhookEndpointRequest,
  type UpdateWebhookEndpointRequest,
  type WebhookEndpointParsed,
} from "./schemas";

export type WebhookEndpoint = WebhookEndpointParsed;

export type WebhookEndpointListResponse = {
  endpoints: WebhookEndpoint[];
};

export type CreateWebhookEndpointInput = CreateWebhookEndpointRequest;
export type UpdateWebhookEndpointInput = UpdateWebhookEndpointRequest;

export const webhooksApi = {
  webhookEndpoints: () =>
    api<WebhookEndpointListResponse>(
      `${apiBasePath}/webhook-endpoints`,
      undefined,
      webhookEndpointListResponseSchema,
    ),
  createWebhookEndpoint: (payload: CreateWebhookEndpointInput) =>
    api<WebhookEndpoint>(
      `${apiBasePath}/webhook-endpoints`,
      {
        method: "POST",
        body: encodeRequest(
          `${apiBasePath}/webhook-endpoints`,
          createWebhookEndpointRequestSchema,
          payload,
        ),
      },
      webhookEndpointSchema,
    ),
  updateWebhookEndpoint: (
    endpointID: string,
    payload: UpdateWebhookEndpointInput,
  ) =>
    api<WebhookEndpoint>(
      `${apiBasePath}/webhook-endpoints/${endpointID}`,
      {
        method: "PUT",
        body: encodeRequest(
          `${apiBasePath}/webhook-endpoints/${endpointID}`,
          updateWebhookEndpointRequestSchema,
          payload,
        ),
      },
      webhookEndpointSchema,
    ),
  deleteWebhookEndpoint: (endpointID: string) =>
    api<void>(`${apiBasePath}/webhook-endpoints/${endpointID}`, {
      method: "DELETE",
    }),
};
