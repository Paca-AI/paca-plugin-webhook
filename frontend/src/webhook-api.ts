import { type PluginApiClient } from "@paca-ai/plugin-sdk-react";

export const PLUGIN_ID = "com.paca.webhook";

// ── Error codes ────────────────────────────────────────────────────────────────

export const ErrorCode = {
  WebhookNotFound: "WEBHOOK_NOT_FOUND",
  WebhookInvalidUrl: "WEBHOOK_INVALID_URL",
  WebhookInvalidEvents: "WEBHOOK_INVALID_EVENTS",
  BadRequest: "BAD_REQUEST",
} as const;

export type ErrorCodeValue = (typeof ErrorCode)[keyof typeof ErrorCode];

/**
 * Extracts the error_code field from a PluginApiClient error.
 * The React SDK throws: `[PluginApiClient] METHOD URL → STATUS: BODY`
 * where BODY is the JSON from the plugin backend.
 */
export function getPluginErrorCode(err: unknown): ErrorCodeValue | null {
  if (!(err instanceof Error)) return null;
  const arrowIdx = err.message.lastIndexOf("→ ");
  if (arrowIdx === -1) return null;
  const rest = err.message.slice(arrowIdx + 2);
  const colonIdx = rest.indexOf(": ");
  if (colonIdx === -1) return null;
  const maybeJson = rest.slice(colonIdx + 2);
  try {
    const body = JSON.parse(maybeJson) as { error_code?: string };
    const code = body.error_code;
    if (!code) return null;
    const known = Object.values(ErrorCode) as string[];
    return known.includes(code) ? (code as ErrorCodeValue) : null;
  } catch {
    return null;
  }
}

// ── Domain types ───────────────────────────────────────────────────────────────

export interface WebhookEventOption {
  topic: string;
  label: string;
  group: string;
}

/** Every activity topic the backend can forward, grouped for the UI. */
export const WEBHOOK_EVENT_OPTIONS: WebhookEventOption[] = [
  { topic: "task.created", label: "Task created", group: "Tasks" },
  { topic: "task.updated", label: "Task updated", group: "Tasks" },
  { topic: "task.deleted", label: "Task deleted", group: "Tasks" },
  {
    topic: "task.attachment.added",
    label: "Attachment added",
    group: "Attachments",
  },
  {
    topic: "task.attachment.removed",
    label: "Attachment removed",
    group: "Attachments",
  },
  { topic: "comment", label: "Comment recorded", group: "Comments" },
  { topic: "task.comment.added", label: "Comment added", group: "Comments" },
  {
    topic: "task.comment.updated",
    label: "Comment updated",
    group: "Comments",
  },
  {
    topic: "task.comment.deleted",
    label: "Comment deleted",
    group: "Comments",
  },
  { topic: "task.link.added", label: "Link added", group: "Links" },
  { topic: "task.link.removed", label: "Link removed", group: "Links" },
  {
    topic: "agent.session.started",
    label: "Agent session started",
    group: "Agent",
  },
];

export interface Webhook {
  id: string;
  project_id: string;
  url: string;
  has_secret: boolean;
  events: string[] | null;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface WebhookDelivery {
  id: string;
  webhook_id: string;
  event_type: string;
  status_code: number;
  success: boolean;
  error: string;
  created_at: string;
}

export interface CreateWebhookInput {
  url: string;
  secret?: string;
  events: string[];
}

export interface UpdateWebhookInput {
  url?: string;
  secret?: string;
  events?: string[];
  enabled?: boolean;
}

// ── Query keys ─────────────────────────────────────────────────────────────────

export const webhooksKey = (projectId: string) => ["webhooks", projectId];
export const webhookDeliveriesKey = (projectId: string, webhookId: string) => [
  "webhooks",
  projectId,
  webhookId,
  "deliveries",
];

// ── API calls ──────────────────────────────────────────────────────────────────

export function listWebhooks(
  api: PluginApiClient,
  projectId: string,
): Promise<Webhook[]> {
  return api.pluginGet<Webhook[]>(PLUGIN_ID, `/projects/${projectId}/webhooks`);
}

export function createWebhook(
  api: PluginApiClient,
  projectId: string,
  input: CreateWebhookInput,
): Promise<Webhook> {
  return api.pluginPost<Webhook>(
    PLUGIN_ID,
    `/projects/${projectId}/webhooks`,
    input,
  );
}

export function updateWebhook(
  api: PluginApiClient,
  projectId: string,
  webhookId: string,
  input: UpdateWebhookInput,
): Promise<Webhook> {
  return api.pluginPatch<Webhook>(
    PLUGIN_ID,
    `/projects/${projectId}/webhooks/${webhookId}`,
    input,
  );
}

export function deleteWebhook(
  api: PluginApiClient,
  projectId: string,
  webhookId: string,
): Promise<void> {
  return api.pluginDelete(
    PLUGIN_ID,
    `/projects/${projectId}/webhooks/${webhookId}`,
  );
}

export function listDeliveries(
  api: PluginApiClient,
  projectId: string,
  webhookId: string,
): Promise<WebhookDelivery[]> {
  return api.pluginGet<WebhookDelivery[]>(
    PLUGIN_ID,
    `/projects/${projectId}/webhooks/${webhookId}/deliveries`,
  );
}

export function testWebhook(
  api: PluginApiClient,
  projectId: string,
  webhookId: string,
): Promise<WebhookDelivery> {
  return api.pluginPost<WebhookDelivery>(
    PLUGIN_ID,
    `/projects/${projectId}/webhooks/${webhookId}/test`,
    {},
  );
}
