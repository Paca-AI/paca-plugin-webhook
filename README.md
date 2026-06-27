# paca-plugin-webhook

Sends HTTP webhooks to a URL of your choice whenever task activity happens in
a Paca project.

## What it does

- Project settings tab (**Webhooks**) lets you configure one or more webhook
  URLs, each with its own optional signing secret and a subset of activity
  events to listen for (task created/updated/deleted, comments, links,
  attachments, agent sessions).
- The backend subscribes to those same activity topics on the Paca event bus
  (`ctx.On(topic, ...)`), which mirrors the topics the API also appends to its
  Valkey task-activity stream — so a webhook fires for exactly the events you'd
  see in the activity stream.
- Each delivery is a `POST` with a JSON body `{"event", "payload", "sent_at"}`,
  signed with `X-Paca-Signature: sha256=<hmac>` when a secret is configured.
- Delivery outcomes (status code, success, error) are recorded per webhook and
  visible in the settings UI.

## Layout

- `backend/` — Go WASM plugin: webhook CRUD routes, event-driven delivery via
  `plugin.Fetch`, AES-256-GCM secret encryption (reuses `ENCRYPTION_KEY`).
- `frontend/` — React settings tab (Vite + Module Federation).
- `plugin.json` — manifest. Sets `allowedOutboundDomains: ["*"]` since webhook
  destinations are arbitrary, user-supplied URLs (the host still enforces
  HTTPS-only and blocks private/internal IPs).
