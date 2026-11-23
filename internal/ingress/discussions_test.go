package ingress

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"grubby/internal/protocol"
)

func TestIngress_DiscussionsWebhooks(t *testing.T) {
	srv, db, bus := setupTestIngress(t)
	defer db.Close()
	defer bus.Close()

	sub := bus.Subscribe(func(e protocol.EventEnvelope) bool {
		return e.Entity == protocol.EntityDiscussion
	})
	defer sub.Unsubscribe()

	// 1. Send discussion created webhook
	discPayload := `{
		"action": "created",
		"discussion": {
			"id": 101,
			"node_id": "D_101",
			"number": 42,
			"title": "Discussion Webhook Test",
			"body": "Hello @simon, testing webhook discussion",
			"created_at": "2026-11-22T10:00:00Z",
			"updated_at": "2026-11-22T10:00:00Z",
			"html_url": "https://github.com/org/tasks/discussions/42",
			"comments": 0,
			"user": {"login": "alice"},
			"category": {"id": 1, "name": "Platform", "slug": "platform"}
		},
		"repository": {
			"name": "tasks",
			"full_name": "org/tasks",
			"owner": {"login": "org"}
		}
	}`

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewBufferString(discPayload))
	req.Header.Set("X-GitHub-Event", "discussion")
	req.Header.Set("X-GitHub-Delivery", "disc-del-1")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	select {
	case evt := <-sub.C:
		if evt.Action != protocol.ActionCreated {
			t.Errorf("expected ActionCreated, got %v", evt.Action)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for discussion created event on bus")
	}

	// Verify saved in DB
	d, err := db.GetDiscussion("D_101")
	if err != nil || d == nil {
		t.Fatalf("expected discussion D_101 in DB, err: %v", err)
	}
	if d.Title != "Discussion Webhook Test" {
		t.Errorf("unexpected title: %s", d.Title)
	}

	// 2. Send discussion_comment created webhook
	cmPayload := `{
		"action": "created",
		"comment": {
			"id": 201,
			"node_id": "DC_201",
			"body": "Reply from bob mentioning @simon",
			"created_at": "2026-11-22T10:05:00Z",
			"updated_at": "2026-11-22T10:05:00Z",
			"html_url": "https://github.com/org/tasks/discussions/42#comment-201",
			"user": {"login": "bob"}
		},
		"discussion": {
			"id": 101,
			"node_id": "D_101",
			"number": 42,
			"title": "Discussion Webhook Test",
			"body": "Hello @simon, testing webhook discussion",
			"created_at": "2026-11-22T10:00:00Z",
			"updated_at": "2026-11-22T10:05:00Z",
			"html_url": "https://github.com/org/tasks/discussions/42",
			"comments": 1,
			"user": {"login": "alice"},
			"category": {"id": 1, "name": "Platform", "slug": "platform"}
		},
		"repository": {
			"name": "tasks",
			"full_name": "org/tasks",
			"owner": {"login": "org"}
		}
	}`

	req2 := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewBufferString(cmPayload))
	req2.Header.Set("X-GitHub-Event", "discussion_comment")
	req2.Header.Set("X-GitHub-Delivery", "disc-del-2")
	rec2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec2.Code)
	}

	select {
	case evt := <-sub.C:
		if evt.Action != protocol.ActionUpdated {
			t.Errorf("expected ActionUpdated, got %v", evt.Action)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for comment event on bus")
	}

	comments, err := db.GetDiscussionComments("D_101")
	if err != nil || len(comments) != 1 {
		t.Fatalf("expected 1 comment in DB, got %d, err: %v", len(comments), err)
	}
	if comments[0].Author != "bob" {
		t.Errorf("unexpected comment author: %s", comments[0].Author)
	}
}
