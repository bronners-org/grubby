package ingress

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"grubby/internal/eventbus"
	"grubby/internal/github"
	"grubby/internal/protocol"
	"grubby/internal/storage"
)

func setupTestIngress(t *testing.T) (*Server, *storage.DB, *eventbus.Bus) {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}

	bus := eventbus.NewBus(50)
	cfg := Config{
		Enabled:          true,
		Port:             8484,
		Path:             "/webhooks/github",
		HomeOrg:          "acme-corp",
		CentralTasksRepo: "acme-corp/grubby-tasks",
	}

	srv := NewServer(cfg, db, bus)
	return srv, db, bus
}

func TestIngressPingAndHealthz(t *testing.T) {
	srv, db, bus := setupTestIngress(t)
	defer db.Close()
	defer bus.Close()

	handler := srv.Handler()

	// 1. Healthz
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 from healthz, got %d", rec.Code)
	}

	// 2. Ping Event
	pingReq := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewBufferString(`{}`))
	pingReq.Header.Set("X-GitHub-Event", "ping")
	pingRec := httptest.NewRecorder()
	handler.ServeHTTP(pingRec, pingReq)
	if pingRec.Code != http.StatusOK {
		t.Errorf("expected 200 from ping event, got %d", pingRec.Code)
	}
}

func TestIngressRepositoryEvent(t *testing.T) {
	srv, db, bus := setupTestIngress(t)
	defer db.Close()
	defer bus.Close()

	sub := bus.Subscribe(nil)
	defer sub.Unsubscribe()

	payload := `{
		"action": "created",
		"repository": {
			"id": 12345,
			"name": "payments-api",
			"full_name": "acme-corp/payments-api",
			"description": "Core payment service",
			"default_branch": "main",
			"private": true,
			"html_url": "https://github.com/acme-corp/payments-api",
			"ssh_url": "git@github.com:acme-corp/payments-api.git",
			"pushed_at": "2026-11-16T08:00:00Z",
			"updated_at": "2026-11-16T08:00:00Z",
			"owner": { "login": "acme-corp" }
		}
	}`

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewBufferString(payload))
	req.Header.Set("X-GitHub-Event", "repository")
	req.Header.Set("X-GitHub-Delivery", "deliv-repo-1")
	rec := httptest.NewRecorder()

	srv.handleWebhook(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	// Verify saved to SQLite
	repo, err := db.GetRepoByName("acme-corp/payments-api")
	if err != nil || repo == nil {
		t.Fatalf("expected repo in db, err: %v", err)
	}
	if !repo.IsHomeOrg {
		t.Errorf("expected acme-corp to be recognized as Home Org")
	}

	// Verify event bus publication
	select {
	case evt := <-sub.C:
		if evt.Entity != protocol.EntityRepository || evt.Action != protocol.ActionCreated {
			t.Errorf("unexpected event: %+v", evt)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for event on bus")
	}
}

func TestIngressPullRequestAndReview(t *testing.T) {
	srv, db, bus := setupTestIngress(t)
	defer db.Close()
	defer bus.Close()

	sub := bus.Subscribe(nil)
	defer sub.Unsubscribe()

	// 1. Pull Request Opened
	prPayload := `{
		"action": "opened",
		"number": 88,
		"pull_request": {
			"number": 88,
			"title": "Fix Stripe Webhook Replay",
			"state": "open",
			"draft": false,
			"html_url": "https://github.com/acme-corp/payments-api/pull/88",
			"created_at": "2026-11-16T08:00:00Z",
			"updated_at": "2026-11-16T08:00:00Z",
			"comments": 1,
			"review_comments": 2,
			"user": { "login": "alice" },
			"head": { "ref": "feat/stripe-fix" },
			"labels": [ { "name": "bug" } ],
			"requested_reviewers": [ { "login": "bob" } ]
		},
		"repository": {
			"name": "payments-api",
			"full_name": "acme-corp/payments-api",
			"owner": { "login": "acme-corp" }
		},
		"sender": { "login": "alice" }
	}`

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewBufferString(prPayload))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-GitHub-Delivery", "deliv-pr-1")
	rec := httptest.NewRecorder()

	srv.handleWebhook(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Verify PR saved in SQLite
	prs, err := db.GetPullRequests()
	if err != nil || len(prs) != 1 {
		t.Fatalf("expected 1 PR in db, got %d", len(prs))
	}
	if prs[0].Number != 88 || prs[0].ReviewDecision != "REVIEW_REQUIRED" {
		t.Errorf("unexpected PR: %+v", prs[0])
	}

	// 2. Pull Request Review Submitted (Approved)
	reviewPayload := `{
		"action": "submitted",
		"review": {
			"state": "approved",
			"html_url": "https://github.com/acme-corp/payments-api/pull/88#review-1",
			"user": { "login": "bob" }
		},
		"pull_request": {
			"number": 88,
			"title": "Fix Stripe Webhook Replay",
			"user": { "login": "alice" }
		},
		"repository": {
			"name": "payments-api",
			"full_name": "acme-corp/payments-api",
			"owner": { "login": "acme-corp" }
		}
	}`

	revReq := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewBufferString(reviewPayload))
	revReq.Header.Set("X-GitHub-Event", "pull_request_review")
	revReq.Header.Set("X-GitHub-Delivery", "deliv-rev-1")
	revRec := httptest.NewRecorder()

	srv.handleWebhook(revRec, revReq)
	if revRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for review, got %d", revRec.Code)
	}

	// Verify reviewDecision updated to APPROVED in SQLite
	prsAfterReview, _ := db.GetPullRequests()
	if len(prsAfterReview) != 1 || prsAfterReview[0].ReviewDecision != "APPROVED" {
		t.Errorf("expected PR reviewDecision to be APPROVED, got %+v", prsAfterReview)
	}
}

func TestIngressIssuesAndTasks(t *testing.T) {
	srv, db, bus := setupTestIngress(t)
	defer db.Close()
	defer bus.Close()

	issuePayload := `{
		"action": "opened",
		"issue": {
			"id": 991122,
			"number": 7,
			"title": "Migrate DB cluster",
			"body": "Prepare read replica",
			"state": "open",
			"html_url": "https://github.com/acme-corp/grubby-tasks/issues/7",
			"created_at": "2026-11-16T08:00:00Z",
			"updated_at": "2026-11-16T08:00:00Z",
			"user": { "login": "dave" },
			"assignees": [ { "login": "dave" } ],
			"labels": [
				{ "name": "priority: high" },
				{ "name": "squad: platform" }
			]
		},
		"repository": {
			"name": "grubby-tasks",
			"full_name": "acme-corp/grubby-tasks",
			"owner": { "login": "acme-corp" }
		},
		"sender": { "login": "dave" }
	}`

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewBufferString(issuePayload))
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-GitHub-Delivery", "deliv-issue-1")
	rec := httptest.NewRecorder()

	srv.handleWebhook(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Verify squad task saved in SQLite
	squadTasks, err := db.GetTasks(true, "open")
	if err != nil || len(squadTasks) != 1 {
		t.Fatalf("expected 1 squad task, got %d (err: %v)", len(squadTasks), err)
	}
	if squadTasks[0].Priority != github.PriorityHigh {
		t.Errorf("expected PriorityHigh, got %s", squadTasks[0].Priority)
	}
	if !squadTasks[0].IsHomeRepo {
		t.Errorf("expected central task repo to be marked IsHomeRepo")
	}
}

func TestIngressDeduplication(t *testing.T) {
	srv, db, bus := setupTestIngress(t)
	defer db.Close()
	defer bus.Close()

	payload := `{
		"action": "created",
		"repository": {
			"id": 555,
			"name": "dup-repo",
			"full_name": "acme-corp/dup-repo",
			"owner": { "login": "acme-corp" }
		}
	}`

	deliveryID := "unique-delivery-123"

	// First request: should be processed
	req1 := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewBufferString(payload))
	req1.Header.Set("X-GitHub-Event", "repository")
	req1.Header.Set("X-GitHub-Delivery", deliveryID)
	rec1 := httptest.NewRecorder()
	srv.handleWebhook(rec1, req1)

	var resp1 map[string]string
	_ = json.Unmarshal(rec1.Body.Bytes(), &resp1)
	if resp1["status"] != "processed" {
		t.Errorf("expected status 'processed', got %s", resp1["status"])
	}

	// Second request with exact same delivery ID: should be skipped!
	req2 := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewBufferString(payload))
	req2.Header.Set("X-GitHub-Event", "repository")
	req2.Header.Set("X-GitHub-Delivery", deliveryID)
	rec2 := httptest.NewRecorder()
	srv.handleWebhook(rec2, req2)

	var resp2 map[string]string
	_ = json.Unmarshal(rec2.Body.Bytes(), &resp2)
	if resp2["status"] != "duplicate_skipped" {
		t.Errorf("expected status 'duplicate_skipped', got %s", resp2["status"])
	}

	// WebhooksProcessed counter should only be 1
	if srv.WebhooksProcessed() != 1 {
		t.Errorf("expected 1 processed webhook, got %d", srv.WebhooksProcessed())
	}
}

func TestIngressSSEEndpoint(t *testing.T) {
	srv, db, bus := setupTestIngress(t)
	defer db.Close()
	defer bus.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.handleSSE(rec, req)
	}()

	// Wait a tiny bit for the subscription to register
	time.Sleep(20 * time.Millisecond)

	// Publish an event on bus
	env, err := protocol.NewRepositoryEvent(protocol.ActionCreated, github.Repository{
		Name:     "new-repo",
		FullName: "acme/new-repo",
		Owner:    "acme",
	})
	if err != nil {
		t.Fatalf("failed to create repo event: %v", err)
	}
	bus.Publish(env)

	// Give SSE handler a moment to flush
	time.Sleep(50 * time.Millisecond)

	// Cancel context to stop SSE stream
	cancel()
	<-done

	body := rec.Body.String()
	if !strings.Contains(body, "event: repository") {
		t.Errorf("expected SSE stream to contain 'event: repository', got:\n%s", body)
	}
	if !strings.Contains(body, "acme/new-repo") {
		t.Errorf("expected SSE stream to contain 'acme/new-repo', got:\n%s", body)
	}
}
