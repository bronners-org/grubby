package protocol

import (
	"encoding/json"
	"fmt"
	"time"

	"grubby/internal/github"
)

type EntityType string

const (
	EntityRepository    EntityType = "repository"
	EntityPullRequest   EntityType = "pull_request"
	EntityTask          EntityType = "task"
	EntitySquadActivity EntityType = "squad_activity"
	EntitySecurityAlert EntityType = "security_alert"
	EntityOrgMember     EntityType = "org_member"
	EntitySyncState     EntityType = "sync_state"
)

type ActionType string

const (
	ActionCreated    ActionType = "created"
	ActionUpdated    ActionType = "updated"
	ActionDeleted    ActionType = "deleted"
	ActionReconciled ActionType = "reconciled"
)

// EventEnvelope is the transport-agnostic event wrapper transmitted via Go channels or SSE.
type EventEnvelope struct {
	ID        string          `json:"id"`
	Org       string          `json:"org"`
	Entity    EntityType      `json:"entity"`
	Action    ActionType      `json:"action"`
	Timestamp time.Time       `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

type RepositoryPayload struct {
	Repo github.Repository `json:"repo"`
}

type PullRequestPayload struct {
	PR github.PullRequest `json:"pr"`
}

type TaskPayload struct {
	Task github.Task `json:"task"`
}

type SquadActivityPayload struct {
	Event github.SquadEvent `json:"event"`
}

type SecurityAlertPayload struct {
	Alert github.SecurityAlertSummary `json:"alert"`
}

type SyncStatePayload struct {
	Org            string `json:"org"`
	Status         string `json:"status"` // "synced", "reconciling", "webhook_received", "health_alert"
	WebhooksCount  int64  `json:"webhooks_count"`
	Message        string `json:"message"`
}

func NewRepositoryEvent(action ActionType, repo github.Repository) (EventEnvelope, error) {
	data, err := json.Marshal(RepositoryPayload{Repo: repo})
	if err != nil {
		return EventEnvelope{}, err
	}
	org := repo.Owner
	if org == "" && repo.FullName != "" {
		org = repo.FullName
	}
	return EventEnvelope{
		ID:        fmt.Sprintf("repo-%s-%d", repo.FullName, time.Now().UnixNano()),
		Org:       org,
		Entity:    EntityRepository,
		Action:    action,
		Timestamp: time.Now(),
		Payload:   data,
	}, nil
}

func NewPullRequestEvent(action ActionType, pr github.PullRequest) (EventEnvelope, error) {
	data, err := json.Marshal(PullRequestPayload{PR: pr})
	if err != nil {
		return EventEnvelope{}, err
	}
	org := pr.RepoOwner
	if org == "" && pr.RepoFullName != "" {
		org = pr.RepoFullName
	}
	return EventEnvelope{
		ID:        fmt.Sprintf("pr-%s-%d-%d", pr.Repo, pr.Number, time.Now().UnixNano()),
		Org:       org,
		Entity:    EntityPullRequest,
		Action:    action,
		Timestamp: time.Now(),
		Payload:   data,
	}, nil
}

func NewTaskEvent(action ActionType, task github.Task) (EventEnvelope, error) {
	data, err := json.Marshal(TaskPayload{Task: task})
	if err != nil {
		return EventEnvelope{}, err
	}
	return EventEnvelope{
		ID:        fmt.Sprintf("task-%d-%d", task.ID, time.Now().UnixNano()),
		Org:       task.RepoOwner,
		Entity:    EntityTask,
		Action:    action,
		Timestamp: time.Now(),
		Payload:   data,
	}, nil
}

func NewSquadActivityEvent(action ActionType, event github.SquadEvent) (EventEnvelope, error) {
	data, err := json.Marshal(SquadActivityPayload{Event: event})
	if err != nil {
		return EventEnvelope{}, err
	}
	return EventEnvelope{
		ID:        fmt.Sprintf("act-%s-%d", event.Actor, time.Now().UnixNano()),
		Entity:    EntitySquadActivity,
		Action:    action,
		Timestamp: time.Now(),
		Payload:   data,
	}, nil
}

func NewSyncStateEvent(org, status, msg string, count int64) (EventEnvelope, error) {
	data, err := json.Marshal(SyncStatePayload{
		Org:           org,
		Status:        status,
		WebhooksCount: count,
		Message:       msg,
	})
	if err != nil {
		return EventEnvelope{}, err
	}
	return EventEnvelope{
		ID:        fmt.Sprintf("sync-%s-%d", status, time.Now().UnixNano()),
		Org:       org,
		Entity:    EntitySyncState,
		Action:    ActionUpdated,
		Timestamp: time.Now(),
		Payload:   data,
	}, nil
}
