package main

import (
	"encoding/json"
	"testing"

	plugin "github.com/Paca-AI/plugin-sdk-go"
	"github.com/Paca-AI/plugin-sdk-go/plugintest"
)

const testProjectID = "project-1"

func setupPlugin(t *testing.T) (*webhookPlugin, *plugintest.Context) {
	t.Helper()
	tc := plugintest.NewContext(t)
	tc.Config.Set("ENCRYPTION_KEY", "3fead24473a9a7bf262857db0b4de648c86de5a29b3b3bb5bfb46875ede0d7de")

	tc.DB.SeedRows("projects", []string{"id"}, [][]any{{testProjectID}})
	tc.DB.SeedRows("webhooks",
		[]string{"id", "project_id", "url", "secret_enc", "events", "enabled", "created_at", "updated_at"},
		nil)
	tc.DB.SeedRows("webhook_deliveries",
		[]string{"id", "webhook_id", "event_type", "status_code", "success", "error", "created_at"},
		nil)

	p := &webhookPlugin{}
	if err := p.Init(tc.PluginContext()); err != nil {
		t.Fatal("Init failed:", err)
	}
	return p, tc
}

func callerReq(params map[string]string) plugintest.Request {
	return plugintest.Request{
		Caller: plugin.CallerIdentity{
			ProjectID:  testProjectID,
			CallerID:   "member-1",
			CallerRole: "PROJECT_MEMBER",
		},
		PathParams: params,
	}
}

func TestCreateAndListWebhook(t *testing.T) {
	_, tc := setupPlugin(t)

	createRes := tc.Call("POST", "/projects/:projectId/webhooks",
		callerReq(map[string]string{"projectId": testProjectID}).WithJSONBody(map[string]any{
			"url":    "https://example.com/hooks/paca",
			"secret": "s3cr3t",
			"events": []string{"task.created", "task.deleted"},
		}))
	if createRes.StatusCode != 201 {
		t.Fatalf("expected 201, got %d: %s", createRes.StatusCode, string(createRes.Body))
	}

	var created struct {
		Data webhook `json:"data"`
	}
	if err := json.Unmarshal(createRes.Body, &created); err != nil {
		t.Fatal(err)
	}
	if created.Data.URL != "https://example.com/hooks/paca" {
		t.Fatalf("unexpected url: %s", created.Data.URL)
	}
	if !created.Data.HasSecret {
		t.Fatal("expected has_secret=true")
	}

	listRes := tc.Call("GET", "/projects/:projectId/webhooks",
		callerReq(map[string]string{"projectId": testProjectID}))
	if listRes.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", listRes.StatusCode)
	}
	var listed struct {
		Data []webhook `json:"data"`
	}
	if err := json.Unmarshal(listRes.Body, &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Data) != 1 {
		t.Fatalf("expected 1 webhook, got %d", len(listed.Data))
	}
}

func TestCreateWebhookRejectsInvalidURLAndEvents(t *testing.T) {
	_, tc := setupPlugin(t)

	res := tc.Call("POST", "/projects/:projectId/webhooks",
		callerReq(map[string]string{"projectId": testProjectID}).WithJSONBody(map[string]any{
			"url":    "http://example.com/hooks",
			"events": []string{"task.created"},
		}))
	if res.StatusCode != 400 {
		t.Fatalf("expected 400 for non-https url, got %d", res.StatusCode)
	}

	res = tc.Call("POST", "/projects/:projectId/webhooks",
		callerReq(map[string]string{"projectId": testProjectID}).WithJSONBody(map[string]any{
			"url":    "https://example.com/hooks",
			"events": []string{"not.a.real.topic"},
		}))
	if res.StatusCode != 400 {
		t.Fatalf("expected 400 for unsupported event, got %d", res.StatusCode)
	}
}

func TestUpdateAndDeleteWebhook(t *testing.T) {
	_, tc := setupPlugin(t)

	createRes := tc.Call("POST", "/projects/:projectId/webhooks",
		callerReq(map[string]string{"projectId": testProjectID}).WithJSONBody(map[string]any{
			"url":    "https://example.com/hooks/paca",
			"events": []string{"task.created"},
		}))
	var created struct {
		Data webhook `json:"data"`
	}
	_ = json.Unmarshal(createRes.Body, &created)
	id := created.Data.ID

	updRes := tc.Call("PATCH", "/projects/:projectId/webhooks/:webhookId",
		callerReq(map[string]string{"projectId": testProjectID, "webhookId": id}).WithJSONBody(map[string]any{
			"enabled": false,
		}))
	if updRes.StatusCode != 200 {
		t.Fatalf("expected 200, got %d: %s", updRes.StatusCode, string(updRes.Body))
	}
	var updated struct {
		Data webhook `json:"data"`
	}
	_ = json.Unmarshal(updRes.Body, &updated)
	if updated.Data.Enabled {
		t.Fatal("expected webhook to be disabled")
	}

	delRes := tc.Call("DELETE", "/projects/:projectId/webhooks/:webhookId",
		callerReq(map[string]string{"projectId": testProjectID, "webhookId": id}))
	if delRes.StatusCode != 204 {
		t.Fatalf("expected 204, got %d", delRes.StatusCode)
	}

	getRes := tc.Call("GET", "/projects/:projectId/webhooks/:webhookId",
		callerReq(map[string]string{"projectId": testProjectID, "webhookId": id}))
	if getRes.StatusCode != 404 {
		t.Fatalf("expected 404 after delete, got %d", getRes.StatusCode)
	}
}

// TestActivityEventDispatchSkipsUnsubscribed verifies that a webhook only
// receives events it is subscribed to, and that dispatch doesn't panic even
// though Fetch always fails outside a WASM runtime (recorded as a failed
// delivery rather than a crash).
func TestActivityEventDispatchSkipsUnsubscribed(t *testing.T) {
	p, tc := setupPlugin(t)

	createRes := tc.Call("POST", "/projects/:projectId/webhooks",
		callerReq(map[string]string{"projectId": testProjectID}).WithJSONBody(map[string]any{
			"url":    "https://example.com/hooks/paca",
			"events": []string{"task.deleted"},
		}))
	var created struct {
		Data webhook `json:"data"`
	}
	_ = json.Unmarshal(createRes.Body, &created)

	payload, _ := json.Marshal(map[string]any{
		"project_id":    testProjectID,
		"task_id":       "task-1",
		"activity_type": "task.created",
	})
	// Not subscribed to task.created — handler must be a no-op (no delivery
	// row inserted, no panic).
	p.handleActivityEvent("task.created")(&plugin.Event{Topic: "task.created", Payload: payload})

	delRes := tc.Call("GET", "/projects/:projectId/webhooks/:webhookId/deliveries",
		callerReq(map[string]string{"projectId": testProjectID, "webhookId": created.Data.ID}))
	var deliveries struct {
		Data []webhookDelivery `json:"data"`
	}
	_ = json.Unmarshal(delRes.Body, &deliveries)
	if len(deliveries.Data) != 0 {
		t.Fatalf("expected no deliveries for unsubscribed topic, got %d", len(deliveries.Data))
	}

	// Subscribed topic — handler should attempt delivery (and record a
	// failed delivery row, since Fetch is unavailable outside WASM).
	p.handleActivityEvent("task.deleted")(&plugin.Event{Topic: "task.deleted", Payload: payload})

	delRes = tc.Call("GET", "/projects/:projectId/webhooks/:webhookId/deliveries",
		callerReq(map[string]string{"projectId": testProjectID, "webhookId": created.Data.ID}))
	_ = json.Unmarshal(delRes.Body, &deliveries)
	if len(deliveries.Data) != 1 {
		t.Fatalf("expected 1 delivery for subscribed topic, got %d", len(deliveries.Data))
	}
	if deliveries.Data[0].Success {
		t.Fatal("expected delivery to fail outside WASM runtime")
	}
}
