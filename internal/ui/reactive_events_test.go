package ui

import (
	"testing"
	"time"

	"grubby/internal/eventbus"
	"grubby/internal/github"
	"grubby/internal/protocol"
)

func TestReactiveEvents_PatchRepository(t *testing.T) {
	m := createTestModel()
	initialCount := len(m.Repos)

	// 1. Create Repository Event
	newRepo := github.Repository{
		Name:        "new-service",
		FullName:    "acme/new-service",
		Owner:       "acme",
		Description: "Brand new reactive microservice",
		PushedAt:    time.Now(),
	}
	createEvt, err := protocol.NewRepositoryEvent(protocol.ActionCreated, newRepo)
	if err != nil {
		t.Fatalf("failed to create repo event: %v", err)
	}

	updatedModel, _ := m.handleEntityEvent(createEvt)
	if len(updatedModel.Repos) != initialCount+1 {
		t.Fatalf("expected %d repos after create, got %d", initialCount+1, len(updatedModel.Repos))
	}
	if updatedModel.Repos[0].Name != "new-service" {
		t.Errorf("expected new repo to be prepended, got %s", updatedModel.Repos[0].Name)
	}

	// 2. Update Repository Event
	newRepo.Description = "Updated description"
	updateEvt, _ := protocol.NewRepositoryEvent(protocol.ActionUpdated, newRepo)
	updatedModel, _ = updatedModel.handleEntityEvent(updateEvt)
	if updatedModel.Repos[0].Description != "Updated description" {
		t.Errorf("expected updated description, got %s", updatedModel.Repos[0].Description)
	}

	// 3. Delete Repository Event
	deleteEvt, _ := protocol.NewRepositoryEvent(protocol.ActionDeleted, newRepo)
	updatedModel, _ = updatedModel.handleEntityEvent(deleteEvt)
	if len(updatedModel.Repos) != initialCount {
		t.Fatalf("expected %d repos after delete, got %d", initialCount, len(updatedModel.Repos))
	}
}

func TestReactiveEvents_PatchPullRequest(t *testing.T) {
	m := createTestModel()
	initialPRCount := len(m.PRs)

	// 1. Open PR Event
	pr := github.PullRequest{
		Number:       42,
		Repo:         "api-gateway",
		RepoFullName: "acme/api-gateway",
		RepoOwner:    "acme",
		Title:        "Feature: Reactive Webhooks",
		Author:       "alice",
		State:        "open",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	openEvt, err := protocol.NewPullRequestEvent(protocol.ActionCreated, pr)
	if err != nil {
		t.Fatalf("failed to create pr event: %v", err)
	}

	updatedModel, _ := m.handleEntityEvent(openEvt)
	if len(updatedModel.PRs) != initialPRCount+1 {
		t.Fatalf("expected %d PRs after open, got %d", initialPRCount+1, len(updatedModel.PRs))
	}
	if updatedModel.PRs[0].Number != 42 {
		t.Errorf("expected PR #42 at front, got #%d", updatedModel.PRs[0].Number)
	}

	// 2. Review Approved Update Event
	pr.ReviewDecision = "APPROVED"
	updateEvt, _ := protocol.NewPullRequestEvent(protocol.ActionUpdated, pr)
	updatedModel, _ = updatedModel.handleEntityEvent(updateEvt)
	if updatedModel.PRs[0].ReviewDecision != "APPROVED" {
		t.Errorf("expected APPROVED review decision, got %s", updatedModel.PRs[0].ReviewDecision)
	}

	// 3. Merged PR Event -> should remove from open PRs
	pr.State = "merged"
	mergeEvt, _ := protocol.NewPullRequestEvent(protocol.ActionUpdated, pr)
	updatedModel, _ = updatedModel.handleEntityEvent(mergeEvt)
	if len(updatedModel.PRs) != initialPRCount {
		t.Fatalf("expected %d open PRs after merge, got %d", initialPRCount, len(updatedModel.PRs))
	}
}

func TestReactiveEvents_PatchTask(t *testing.T) {
	m := createTestModel()

	// 1. Create Squad Task
	task := github.Task{
		ID:          999,
		Number:      10,
		Title:       "Set up Hookdeck endpoint",
		RepoName:    "central-tasks",
		RepoOwner:   "acme",
		IsSquadTask: true,
		State:       "open",
		Priority:    github.PriorityHigh,
	}
	createEvt, err := protocol.NewTaskEvent(protocol.ActionCreated, task)
	if err != nil {
		t.Fatalf("failed to create task event: %v", err)
	}

	updatedModel, _ := m.handleEntityEvent(createEvt)
	if len(updatedModel.SquadTasks) != 1 {
		t.Fatalf("expected 1 squad task, got %d", len(updatedModel.SquadTasks))
	}
	if updatedModel.SquadTasks[0].ID != 999 {
		t.Errorf("expected task ID 999, got %d", updatedModel.SquadTasks[0].ID)
	}

	// 2. Close Squad Task -> moves to SquadDoneTasks
	task.State = "closed"
	closeEvt, _ := protocol.NewTaskEvent(protocol.ActionUpdated, task)
	updatedModel, _ = updatedModel.handleEntityEvent(closeEvt)
	if len(updatedModel.SquadTasks) != 0 {
		t.Errorf("expected 0 open squad tasks, got %d", len(updatedModel.SquadTasks))
	}
	if len(updatedModel.SquadDoneTasks) != 1 {
		t.Errorf("expected 1 closed squad task, got %d", len(updatedModel.SquadDoneTasks))
	}

	// 3. Delete Task -> removes from all slices
	deleteEvt, _ := protocol.NewTaskEvent(protocol.ActionDeleted, task)
	updatedModel, _ = updatedModel.handleEntityEvent(deleteEvt)
	if len(updatedModel.SquadDoneTasks) != 0 {
		t.Errorf("expected 0 closed squad tasks after delete, got %d", len(updatedModel.SquadDoneTasks))
	}
}

func TestReactiveEvents_PatchSquadActivity(t *testing.T) {
	m := createTestModel()

	evt := github.SquadEvent{
		Actor:     "bob",
		Action:    "pushed 2 commits",
		Repo:      "auth-service",
		Timestamp: time.Now(),
	}
	actEvt, err := protocol.NewSquadActivityEvent(protocol.ActionCreated, evt)
	if err != nil {
		t.Fatalf("failed to create squad activity event: %v", err)
	}

	updatedModel, _ := m.handleEntityEvent(actEvt)
	if len(updatedModel.SquadEvents) != 1 {
		t.Fatalf("expected 1 squad event, got %d", len(updatedModel.SquadEvents))
	}
	if len(updatedModel.AllCommitEvents) != 1 {
		t.Errorf("expected 1 commit event, got %d", len(updatedModel.AllCommitEvents))
	}
}

func TestReactiveEvents_SetEventBus(t *testing.T) {
	m := createTestModel()
	bus := eventbus.NewBus(10)
	defer bus.Close()

	m.SetEventBus(bus)
	if m.EventBus != bus {
		t.Errorf("expected EventBus to be set")
	}
	if m.EventSub == nil {
		t.Fatalf("expected EventSub to be non-nil after SetEventBus")
	}

	// Init should return a cmd containing listenForEventsCmd
	cmd := m.Init()
	if cmd == nil {
		t.Errorf("expected non-nil Init cmd")
	}
}
