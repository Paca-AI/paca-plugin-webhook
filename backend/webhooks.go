package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	plugin "github.com/Paca-AI/plugin-sdk-go"
)

// ── Domain types ──────────────────────────────────────────────────────────────

type webhook struct {
	ID        string   `json:"id"`
	ProjectID string   `json:"project_id"`
	URL       string   `json:"url"`
	HasSecret bool     `json:"has_secret"`
	Events    []string `json:"events"`
	Enabled   bool     `json:"enabled"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

type webhookDelivery struct {
	ID         string `json:"id"`
	WebhookID  string `json:"webhook_id"`
	EventType  string `json:"event_type"`
	StatusCode int    `json:"status_code"`
	Success    bool   `json:"success"`
	Error      string `json:"error"`
	CreatedAt  string `json:"created_at"`
}

// ── Row scanner helper ────────────────────────────────────────────────────────

type scanner struct {
	idx map[string]int
	row []any
}

func newRowScanner(cols []string, row []any) *scanner {
	idx := make(map[string]int, len(cols))
	for i, c := range cols {
		idx[c] = i
	}
	return &scanner{idx: idx, row: row}
}

func (s *scanner) str(col string) string {
	i, ok := s.idx[col]
	if !ok || i >= len(s.row) || s.row[i] == nil {
		return ""
	}
	switch v := s.row[i].(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func (s *scanner) boolean(col string) bool {
	i, ok := s.idx[col]
	if !ok || i >= len(s.row) || s.row[i] == nil {
		return false
	}
	if v, ok := s.row[i].(bool); ok {
		return v
	}
	return false
}

func (s *scanner) intVal(col string) int {
	i, ok := s.idx[col]
	if !ok || i >= len(s.row) || s.row[i] == nil {
		return 0
	}
	switch v := s.row[i].(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}

func rowToWebhook(sc *scanner) webhook {
	events := []string{}
	_ = json.Unmarshal([]byte(sc.str("events")), &events)
	if events == nil {
		events = []string{}
	}
	return webhook{
		ID:        sc.str("id"),
		ProjectID: sc.str("project_id"),
		URL:       sc.str("url"),
		HasSecret: sc.str("secret_enc") != "",
		Events:    events,
		Enabled:   sc.boolean("enabled"),
		CreatedAt: sc.str("created_at"),
		UpdatedAt: sc.str("updated_at"),
	}
}

func rowToDelivery(sc *scanner) webhookDelivery {
	return webhookDelivery{
		ID:         sc.str("id"),
		WebhookID:  sc.str("webhook_id"),
		EventType:  sc.str("event_type"),
		StatusCode: sc.intVal("status_code"),
		Success:    sc.boolean("success"),
		Error:      sc.str("error"),
		CreatedAt:  sc.str("created_at"),
	}
}

// ── Validation helpers ────────────────────────────────────────────────────────

func isValidWebhookURL(raw string) bool {
	if raw == "" {
		return false
	}
	return len(raw) > len("https://") && raw[:8] == "https://"
}

func isSupportedTopic(topic string) bool {
	for _, t := range supportedTopics {
		if t == topic {
			return true
		}
	}
	return false
}

func sanitizeEvents(in []string) ([]string, error) {
	if len(in) == 0 {
		return nil, fmt.Errorf("at least one event must be selected")
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, e := range in {
		if !isSupportedTopic(e) {
			return nil, fmt.Errorf("unsupported event: %s", e)
		}
		if seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	return out, nil
}

// ── Webhook CRUD handlers ─────────────────────────────────────────────────────

// listWebhooks handles GET /projects/:projectId/webhooks.
func (p *webhookPlugin) listWebhooks(req *plugin.Request, res *plugin.Response) {
	projectID := req.PathParam("projectId")

	result, err := p.db.Query(
		`SELECT id, project_id, url, secret_enc, events, enabled, created_at, updated_at
		 FROM webhooks WHERE project_id = $1 ORDER BY created_at`,
		projectID,
	)
	if err != nil {
		p.log.Error("listWebhooks: " + err.Error())
		apiError(res, 500, "INTERNAL_ERROR", "failed to list webhooks")
		return
	}
	hooks := make([]webhook, 0, len(result.Rows))
	for _, row := range result.Rows {
		hooks = append(hooks, rowToWebhook(newRowScanner(result.Columns, row)))
	}
	ok(res, hooks)
}

// createWebhook handles POST /projects/:projectId/webhooks.
func (p *webhookPlugin) createWebhook(req *plugin.Request, res *plugin.Response) {
	projectID := req.PathParam("projectId")

	type body struct {
		URL    string   `json:"url"`
		Secret string   `json:"secret"`
		Events []string `json:"events"`
	}
	b, err := plugin.JSONBody[body](req)
	if err != nil {
		apiError(res, 400, "BAD_REQUEST", "invalid request body")
		return
	}
	if !isValidWebhookURL(b.URL) {
		apiError(res, 400, "WEBHOOK_INVALID_URL", "url must be an https:// URL")
		return
	}
	events, err := sanitizeEvents(b.Events)
	if err != nil {
		apiError(res, 400, "WEBHOOK_INVALID_EVENTS", err.Error())
		return
	}

	secretEnc, err := p.encrypt(b.Secret)
	if err != nil {
		p.log.Error("createWebhook encrypt: " + err.Error())
		apiError(res, 500, "INTERNAL_ERROR", "failed to secure webhook secret")
		return
	}
	eventsJSON, _ := json.Marshal(events)
	now := nowStr()

	inserted, err := p.db.Query(
		`INSERT INTO webhooks (project_id, url, secret_enc, events, enabled, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id`,
		projectID, b.URL, secretEnc, string(eventsJSON), true, now, now,
	)
	if err != nil || len(inserted.Rows) == 0 {
		if err != nil {
			p.log.Error("createWebhook insert: " + err.Error())
		}
		apiError(res, 500, "INTERNAL_ERROR", "failed to create webhook")
		return
	}
	id := newRowScanner(inserted.Columns, inserted.Rows[0]).str("id")

	created(res, webhook{
		ID:        id,
		ProjectID: projectID,
		URL:       b.URL,
		HasSecret: secretEnc != "",
		Events:    events,
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	})
}

// getWebhook handles GET /projects/:projectId/webhooks/:webhookId.
func (p *webhookPlugin) getWebhook(req *plugin.Request, res *plugin.Response) {
	projectID := req.PathParam("projectId")
	webhookID := req.PathParam("webhookId")

	sc, ok2 := p.fetchWebhookRow(webhookID, projectID, res)
	if !ok2 {
		return
	}
	ok(res, rowToWebhook(sc))
}

// updateWebhook handles PATCH /projects/:projectId/webhooks/:webhookId.
func (p *webhookPlugin) updateWebhook(req *plugin.Request, res *plugin.Response) {
	projectID := req.PathParam("projectId")
	webhookID := req.PathParam("webhookId")

	sc, ok2 := p.fetchWebhookRow(webhookID, projectID, res)
	if !ok2 {
		return
	}

	type body struct {
		URL     *string  `json:"url"`
		Secret  *string  `json:"secret"`
		Events  []string `json:"events"`
		Enabled *bool    `json:"enabled"`
	}
	b, err := plugin.JSONBody[body](req)
	if err != nil {
		apiError(res, 400, "BAD_REQUEST", "invalid request body")
		return
	}

	url := sc.str("url")
	if b.URL != nil {
		if !isValidWebhookURL(*b.URL) {
			apiError(res, 400, "WEBHOOK_INVALID_URL", "url must be an https:// URL")
			return
		}
		url = *b.URL
	}

	secretEnc := sc.str("secret_enc")
	if b.Secret != nil {
		secretEnc, err = p.encrypt(*b.Secret)
		if err != nil {
			p.log.Error("updateWebhook encrypt: " + err.Error())
			apiError(res, 500, "INTERNAL_ERROR", "failed to secure webhook secret")
			return
		}
	}

	events := []string{}
	_ = json.Unmarshal([]byte(sc.str("events")), &events)
	if events == nil {
		events = []string{}
	}
	if b.Events != nil {
		events, err = sanitizeEvents(b.Events)
		if err != nil {
			apiError(res, 400, "WEBHOOK_INVALID_EVENTS", err.Error())
			return
		}
	}

	enabled := sc.boolean("enabled")
	if b.Enabled != nil {
		enabled = *b.Enabled
	}

	eventsJSON, _ := json.Marshal(events)
	now := nowStr()
	_, err = p.db.Exec(
		`UPDATE webhooks SET url = $1, secret_enc = $2, events = $3, enabled = $4, updated_at = $5
		 WHERE id = $6 AND project_id = $7`,
		url, secretEnc, string(eventsJSON), enabled, now, webhookID, projectID,
	)
	if err != nil {
		p.log.Error("updateWebhook: " + err.Error())
		apiError(res, 500, "INTERNAL_ERROR", "failed to update webhook")
		return
	}

	ok(res, webhook{
		ID:        webhookID,
		ProjectID: projectID,
		URL:       url,
		HasSecret: secretEnc != "",
		Events:    events,
		Enabled:   enabled,
		CreatedAt: sc.str("created_at"),
		UpdatedAt: now,
	})
}

// deleteWebhook handles DELETE /projects/:projectId/webhooks/:webhookId.
func (p *webhookPlugin) deleteWebhook(req *plugin.Request, res *plugin.Response) {
	projectID := req.PathParam("projectId")
	webhookID := req.PathParam("webhookId")

	affected, err := p.db.Exec(
		`DELETE FROM webhooks WHERE id = $1 AND project_id = $2`,
		webhookID, projectID,
	)
	if err != nil {
		p.log.Error("deleteWebhook: " + err.Error())
		apiError(res, 500, "INTERNAL_ERROR", "failed to delete webhook")
		return
	}
	if affected == 0 {
		apiError(res, 404, "WEBHOOK_NOT_FOUND", "webhook not found")
		return
	}
	res.NoContent()
}

// listDeliveries handles GET /projects/:projectId/webhooks/:webhookId/deliveries.
func (p *webhookPlugin) listDeliveries(req *plugin.Request, res *plugin.Response) {
	projectID := req.PathParam("projectId")
	webhookID := req.PathParam("webhookId")

	if _, ok2 := p.fetchWebhookRow(webhookID, projectID, res); !ok2 {
		return
	}

	result, err := p.db.Query(
		`SELECT id, webhook_id, event_type, status_code, success, error, created_at
		 FROM webhook_deliveries WHERE webhook_id = $1
		 ORDER BY created_at DESC LIMIT 50`,
		webhookID,
	)
	if err != nil {
		p.log.Error("listDeliveries: " + err.Error())
		apiError(res, 500, "INTERNAL_ERROR", "failed to list deliveries")
		return
	}
	deliveries := make([]webhookDelivery, 0, len(result.Rows))
	for _, row := range result.Rows {
		deliveries = append(deliveries, rowToDelivery(newRowScanner(result.Columns, row)))
	}
	ok(res, deliveries)
}

// testWebhook handles POST /projects/:projectId/webhooks/:webhookId/test.
// Sends a synthetic "webhook.test" event to verify the URL/secret are wired
// up correctly, without waiting for a real task activity.
func (p *webhookPlugin) testWebhook(req *plugin.Request, res *plugin.Response) {
	projectID := req.PathParam("projectId")
	webhookID := req.PathParam("webhookId")

	sc, ok2 := p.fetchWebhookRow(webhookID, projectID, res)
	if !ok2 {
		return
	}

	payload := map[string]any{
		"project_id": projectID,
		"message":    "This is a test event from Paca webhooks.",
		"sent_at":    nowStr(),
	}
	delivery := p.deliver(sc, "webhook.test", payload)
	if delivery.Success {
		ok(res, delivery)
	} else {
		res.JSON(502, envelope{Success: false, Data: delivery})
	}
}

// fetchWebhookRow loads a webhook row scoped to projectID, writing a 404
// response and returning ok=false when it does not exist.
func (p *webhookPlugin) fetchWebhookRow(webhookID, projectID string, res *plugin.Response) (*scanner, bool) {
	result, err := p.db.Query(
		`SELECT id, project_id, url, secret_enc, events, enabled, created_at, updated_at
		 FROM webhooks WHERE id = $1 AND project_id = $2`,
		webhookID, projectID,
	)
	if err != nil {
		p.log.Error("fetchWebhookRow: " + err.Error())
		apiError(res, 500, "INTERNAL_ERROR", "internal error")
		return nil, false
	}
	if len(result.Rows) == 0 {
		apiError(res, 404, "WEBHOOK_NOT_FOUND", "webhook not found")
		return nil, false
	}
	return newRowScanner(result.Columns, result.Rows[0]), true
}

// ── Event dispatch ────────────────────────────────────────────────────────────

// handleActivityEvent returns an EventHandler closure that delivers topic to
// every enabled webhook in the activity's project that is subscribed to it.
func (p *webhookPlugin) handleActivityEvent(topic string) plugin.EventHandler {
	return func(evt *plugin.Event) {
		var payload map[string]any
		if err := json.Unmarshal(evt.Payload, &payload); err != nil {
			p.log.Warn("webhook: failed to decode event payload for topic " + topic)
			return
		}
		projectID, _ := payload["project_id"].(string)
		if projectID == "" {
			return
		}

		result, err := p.db.Query(
			`SELECT id, project_id, url, secret_enc, events, enabled, created_at, updated_at
			 FROM webhooks WHERE project_id = $1`,
			projectID,
		)
		if err != nil {
			p.log.Error("webhook: load webhooks for event: " + err.Error())
			return
		}

		for _, row := range result.Rows {
			sc := newRowScanner(result.Columns, row)
			if !sc.boolean("enabled") {
				continue
			}
			var events []string
			_ = json.Unmarshal([]byte(sc.str("events")), &events)
			if !containsTopic(events, topic) {
				continue
			}
			p.deliver(sc, topic, payload)
		}
	}
}

func containsTopic(events []string, topic string) bool {
	for _, e := range events {
		if e == topic {
			return true
		}
	}
	return false
}

// fieldChange mirrors taskdom.FieldChange — the shape the API embeds in a
// task.updated activity's content as `{"changes": [...]}`.
type fieldChange struct {
	Field string `json:"field"`
	Old   any    `json:"old"`
	New   any    `json:"new"`
}

// buildEventData decodes the activity's topic-specific "content" JSON (a
// string, as recorded by the activity log) into a structured "data" object
// for the delivery payload, and builds a short human-readable summary line —
// prefixed with the actor's name — for the envelope's "text" field. Each
// topic has its own content shape, so dispatch on topic rather than trying
// to interpret it generically.
func (p *webhookPlugin) buildEventData(topic string, payload map[string]any) (map[string]any, string) {
	rawContent, _ := payload["content"].(string)
	actor := p.actorName(topic, payload)

	switch topic {
	case "task.created":
		var c struct {
			Title string `json:"title"`
		}
		_ = json.Unmarshal([]byte(rawContent), &c)
		if c.Title == "" {
			return map[string]any{}, fmt.Sprintf("%s created a task", actor)
		}
		return map[string]any{"title": c.Title}, fmt.Sprintf("%s created a task: %q", actor, c.Title)

	case "task.updated":
		ref := p.taskRef(payload)
		var c struct {
			Changes []fieldChange `json:"changes"`
		}
		_ = json.Unmarshal([]byte(rawContent), &c)
		if len(c.Changes) == 0 {
			return map[string]any{"changes": []fieldChange{}}, fmt.Sprintf("%s updated %s", actor, ref)
		}
		parts := make([]string, 0, len(c.Changes))
		for i, ch := range c.Changes {
			// description is stored as BlockNote blocks (same shape as a
			// comment's content) — render it as plain text in the structured
			// data, but keep the summary line short rather than dumping the
			// full before/after text.
			if ch.Field == "description" {
				ch.Old = blockContentToText(ch.Old)
				ch.New = blockContentToText(ch.New)
				c.Changes[i] = ch
				parts = append(parts, "description")
				continue
			}
			parts = append(parts, fmt.Sprintf("%s: %v → %v", ch.Field, ch.Old, ch.New))
		}
		return map[string]any{"changes": c.Changes}, fmt.Sprintf("%s updated %s — %s", actor, ref, strings.Join(parts, ", "))

	case "task.deleted":
		return map[string]any{}, fmt.Sprintf("%s deleted %s", actor, p.taskRef(payload))

	case "task.link.added":
		ref := p.taskRef(payload)
		var c struct {
			TargetTaskID string `json:"target_task_id"`
			LinkType     string `json:"link_type"`
		}
		_ = json.Unmarshal([]byte(rawContent), &c)
		return map[string]any{"target_task_id": c.TargetTaskID, "link_type": c.LinkType},
			fmt.Sprintf("%s linked %s to %s (%s)", actor, ref, c.TargetTaskID, c.LinkType)

	case "task.link.removed":
		var c struct {
			LinkID string `json:"link_id"`
		}
		_ = json.Unmarshal([]byte(rawContent), &c)
		return map[string]any{"link_id": c.LinkID}, fmt.Sprintf("%s removed a link from %s", actor, p.taskRef(payload))

	case "task.comment.deleted":
		commentID, _ := payload["id"].(string)
		return map[string]any{"comment_id": commentID}, fmt.Sprintf("%s deleted a comment on %s", actor, p.taskRef(payload))

	case "task.comment.added", "task.comment.updated", "comment":
		ref := p.taskRef(payload)
		commentID, _ := payload["id"].(string)
		text := extractBlockText(rawContent)
		data := map[string]any{"comment_id": commentID, "text": text}
		if topic == "task.comment.updated" {
			return data, fmt.Sprintf("%s updated a comment on %s", actor, ref)
		}
		return data, fmt.Sprintf("%s commented on %s", actor, ref)

	case "agent.session.started":
		var c struct {
			ConversationID string `json:"conversation_id"`
			AgentID        string `json:"agent_id"`
		}
		_ = json.Unmarshal([]byte(rawContent), &c)
		return map[string]any{"conversation_id": c.ConversationID, "agent_id": c.AgentID},
			fmt.Sprintf("%s started an agent session on %s", actor, p.taskRef(payload))

	case "task.attachment.added":
		return map[string]any{}, fmt.Sprintf("%s added an attachment to %s", actor, p.taskRef(payload))

	case "task.attachment.removed":
		return map[string]any{}, fmt.Sprintf("%s removed an attachment from %s", actor, p.taskRef(payload))

	case "webhook.test":
		msg, _ := payload["message"].(string)
		return map[string]any{"message": msg}, msg

	default:
		return map[string]any{}, fmt.Sprintf("Paca event: %s", topic)
	}
}

// actorName resolves a display name for the activity's actor. The ID space
// in payload["actor_id"] depends on topic: task-level activities record the
// authenticated user's ID, while comment activities record the
// project_members row ID instead — so the join differs by topic. An AI agent
// actor is recorded separately in payload["actor_agent_id"].
func (p *webhookPlugin) actorName(topic string, payload map[string]any) string {
	if agentID, ok := payload["actor_agent_id"].(string); ok && agentID != "" {
		if name := p.lookupName(`SELECT name FROM agents WHERE id = $1`, agentID); name != "" {
			return name
		}
		return "An agent"
	}
	actorID, _ := payload["actor_id"].(string)
	if actorID == "" {
		return "Someone"
	}

	var name string
	switch topic {
	case "task.comment.added", "task.comment.updated", "task.comment.deleted", "comment":
		name = p.lookupName(
			`SELECT u.full_name AS name FROM project_members pm JOIN users u ON u.id = pm.user_id WHERE pm.id = $1`,
			actorID,
		)
	default:
		name = p.lookupName(`SELECT full_name AS name FROM users WHERE id = $1`, actorID)
	}
	if name == "" {
		return "Someone"
	}
	return name
}

// lookupName runs a single-row, single-column ("name") query and returns its
// value, or "" when the query fails or matches nothing.
func (p *webhookPlugin) lookupName(sqlStr, param string) string {
	result, err := p.db.Query(sqlStr, param)
	if err != nil || len(result.Rows) == 0 {
		return ""
	}
	return newRowScanner(result.Columns, result.Rows[0]).str("name")
}

// taskRef returns a quoted reference to the task ("task \"Fix login bug\"")
// for use in a summary line, falling back to "a task" when the title can't
// be resolved (e.g. task_id missing from the payload).
func (p *webhookPlugin) taskRef(payload map[string]any) string {
	taskID, _ := payload["task_id"].(string)
	if taskID == "" {
		return "a task"
	}
	title := p.lookupName(`SELECT title AS name FROM tasks WHERE id = $1`, taskID)
	if title == "" {
		return "a task"
	}
	return fmt.Sprintf("task %q", title)
}

// blockContentToText converts a field-change value (decoded from JSON into
// an `any` by fieldChange) back into BlockNote JSON text and extracts plain
// text from it. Used for fields like "description" that store BlockNote
// blocks, same as comment content.
func blockContentToText(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		// Already plain text (e.g. legacy {"text": "..."} unmarshaled as a
		// bare string) or empty — nothing further to extract.
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return extractBlockText(string(b))
}

// extractBlockText pulls plain text out of BlockNote-shaped content, used for
// both comment content and the task description field. The shape is either a
// BlockNote block array (`[{"content":[{"text":"..."}]}]`) or the legacy
// `{"text": "..."}` object. Mirrors the API's own extractTextFromBlocks so
// webhook text matches what's shown in the UI.
func extractBlockText(raw string) string {
	var blocks []struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal([]byte(raw), &blocks) == nil && len(blocks) > 0 {
		parts := make([]string, 0, len(blocks))
		for _, b := range blocks {
			for _, c := range b.Content {
				if c.Text != "" {
					parts = append(parts, c.Text)
				}
			}
		}
		return strings.Join(parts, " ")
	}
	var legacy struct {
		Text string `json:"text"`
	}
	if json.Unmarshal([]byte(raw), &legacy) == nil {
		return legacy.Text
	}
	return ""
}

// taskURL builds a link to the task on the Paca web app, using the host's
// PUBLIC_URL config value. Returns "" when PUBLIC_URL isn't configured or
// the event has no task_id/project_id (e.g. webhook.test).
func (p *webhookPlugin) taskURL(payload map[string]any) string {
	base, ok := p.cfg.Get("PUBLIC_URL")
	if !ok || base == "" {
		return ""
	}
	projectID, _ := payload["project_id"].(string)
	taskID, _ := payload["task_id"].(string)
	if projectID == "" || taskID == "" {
		return ""
	}
	return strings.TrimRight(base, "/") + "/projects/" + projectID + "/tasks/" + taskID
}

// deliver signs and POSTs the event to the webhook's URL, recording the
// outcome in webhook_deliveries. Errors reaching the remote URL are treated
// as delivery failures, not plugin errors.
func (p *webhookPlugin) deliver(sc *scanner, eventType string, payload map[string]any) webhookDelivery {
	webhookID := sc.str("id")
	targetURL := sc.str("url")

	data, text := p.buildEventData(eventType, payload)
	if url := p.taskURL(payload); url != "" {
		data["url"] = url
		text = text + " - " + url
	}
	body, _ := json.Marshal(map[string]any{
		"event":       eventType,
		"webhook_id":  webhookID,
		"text":        text,
		"task_id":     payload["task_id"],
		"project_id":  payload["project_id"],
		"actor_id":    payload["actor_id"],
		"occurred_at": payload["created_at"],
		"data":        data,
		"sent_at":     nowStr(),
	})

	headers := map[string]string{"Content-Type": "application/json"}
	if secretEnc := sc.str("secret_enc"); secretEnc != "" {
		secret, err := p.decrypt(secretEnc)
		if err == nil && secret != "" {
			mac := hmac.New(sha256.New, []byte(secret))
			mac.Write(body)
			headers["X-Paca-Signature"] = "sha256=" + hex.EncodeToString(mac.Sum(nil))
		}
	}

	resp, err := plugin.Fetch("POST", targetURL, headers, string(body))
	now := nowStr()
	d := webhookDelivery{
		WebhookID: webhookID,
		EventType: eventType,
		CreatedAt: now,
	}
	if err != nil {
		d.Error = err.Error()
	} else {
		d.StatusCode = resp.Status
		d.Success = resp.Status >= 200 && resp.Status < 300
		if !d.Success {
			d.Error = fmt.Sprintf("unexpected status %d", resp.Status)
		}
	}

	inserted, insErr := p.db.Query(
		`INSERT INTO webhook_deliveries (webhook_id, event_type, status_code, success, error, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id`,
		webhookID, eventType, d.StatusCode, d.Success, d.Error, now,
	)
	if insErr != nil {
		p.log.Error("webhook: record delivery: " + insErr.Error())
	} else if len(inserted.Rows) > 0 {
		d.ID = newRowScanner(inserted.Columns, inserted.Rows[0]).str("id")
	}
	return d
}
