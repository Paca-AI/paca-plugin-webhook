// Package main implements the com.paca.webhook backend WASM plugin.
//
// It lets a project configure one or more webhook URLs, each subscribed to a
// subset of task activity topics. Whenever the API records a task activity
// (created/updated/deleted, comments, links, attachments, agent sessions),
// the plugin runtime dispatches the same topic/payload to this plugin via
// HandleEvent, and the plugin signs and POSTs it to every matching, enabled
// webhook for that project.
package main

import (
	"time"

	plugin "github.com/Paca-AI/plugin-sdk-go"
)

// supportedTopics lists every activity topic this plugin can forward. Must
// stay in sync with plugin.json's backend.eventSubscriptions.
var supportedTopics = []string{
	"task.created",
	"task.updated",
	"task.deleted",
	"task.attachment.added",
	"task.attachment.removed",
	"comment",
	"task.link.added",
	"task.link.removed",
	"agent.session.started",
	"task.comment.added",
	"task.comment.updated",
	"task.comment.deleted",
}

// nowStr returns the current UTC time as an RFC3339Nano string.
func nowStr() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// webhookPlugin implements plugin.Plugin.
type webhookPlugin struct {
	db  *plugin.DB
	log *plugin.Logger
	cfg *plugin.Config
}

// Init registers all routes and event handlers on the provided context.
func (p *webhookPlugin) Init(ctx *plugin.Context) error {
	p.db = ctx.DB()
	p.log = ctx.Log()
	p.cfg = ctx.Config()

	for _, topic := range supportedTopics {
		ctx.On(topic, p.handleActivityEvent(topic))
	}

	ctx.Route("GET", "/projects/:projectId/webhooks", p.listWebhooks)
	ctx.Route("POST", "/projects/:projectId/webhooks", p.createWebhook)
	ctx.Route("GET", "/projects/:projectId/webhooks/:webhookId", p.getWebhook)
	ctx.Route("PATCH", "/projects/:projectId/webhooks/:webhookId", p.updateWebhook)
	ctx.Route("DELETE", "/projects/:projectId/webhooks/:webhookId", p.deleteWebhook)
	ctx.Route("GET", "/projects/:projectId/webhooks/:webhookId/deliveries", p.listDeliveries)
	ctx.Route("POST", "/projects/:projectId/webhooks/:webhookId/test", p.testWebhook)

	return nil
}

// Shutdown is a no-op for this plugin.
func (p *webhookPlugin) Shutdown() {}

// ─── envelope helpers ────────────────────────────────────────────────────────

type envelope struct {
	Success bool `json:"success"`
	Data    any  `json:"data"`
}

func ok(res *plugin.Response, data any) {
	res.JSON(200, envelope{Success: true, Data: data})
}

func created(res *plugin.Response, data any) {
	res.JSON(201, envelope{Success: true, Data: data})
}

func apiError(res *plugin.Response, code int, errCode, message string) {
	res.JSON(code, map[string]any{
		"success":    false,
		"error":      message,
		"error_code": errCode,
	})
}
