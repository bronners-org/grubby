package eventbus

import (
	"sync/atomic"
	"testing"
	"time"

	"grubby/internal/github"
	"grubby/internal/protocol"
)

func TestEventBusPubSub(t *testing.T) {
	bus := NewBus(10)
	defer bus.Close()

	sub := bus.Subscribe(nil)
	defer sub.Unsubscribe()

	repo := github.Repository{
		Name:     "test-repo",
		FullName: "acme/test-repo",
		Owner:    "acme",
	}
	evt, err := protocol.NewRepositoryEvent(protocol.ActionCreated, repo)
	if err != nil {
		t.Fatalf("NewRepositoryEvent failed: %v", err)
	}

	bus.Publish(evt)

	select {
	case received := <-sub.C:
		if received.Entity != protocol.EntityRepository {
			t.Errorf("expected EntityRepository, got %s", received.Entity)
		}
		if received.Action != protocol.ActionCreated {
			t.Errorf("expected ActionCreated, got %s", received.Action)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for event on subscription channel")
	}
}

func TestEventBusFiltering(t *testing.T) {
	bus := NewBus(10)
	defer bus.Close()

	// Only subscribe to PR events
	prSub := bus.Subscribe(func(e protocol.EventEnvelope) bool {
		return e.Entity == protocol.EntityPullRequest
	})
	defer prSub.Unsubscribe()

	// Publish Repo event
	repoEvt, _ := protocol.NewRepositoryEvent(protocol.ActionCreated, github.Repository{Name: "r"})
	bus.Publish(repoEvt)

	// Publish PR event
	prEvt, _ := protocol.NewPullRequestEvent(protocol.ActionCreated, github.PullRequest{Number: 42, Repo: "r"})
	bus.Publish(prEvt)

	select {
	case received := <-prSub.C:
		if received.Entity != protocol.EntityPullRequest {
			t.Errorf("expected only PullRequest event, got %s", received.Entity)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for filtered event")
	}

	// Verify no more events
	select {
	case unexpected := <-prSub.C:
		t.Errorf("unexpected event received: %+v", unexpected)
	case <-time.After(50 * time.Millisecond):
		// OK
	}
}

func TestEventBusUnsubscribe(t *testing.T) {
	bus := NewBus(10)
	defer bus.Close()

	sub := bus.Subscribe(nil)
	sub.Unsubscribe()

	// Channel should be closed
	_, ok := <-sub.C
	if ok {
		t.Errorf("expected channel to be closed after Unsubscribe")
	}
}

func TestEventBusBufferOverflowNonBlocking(t *testing.T) {
	// Tiny buffer of 1
	bus := NewBus(1)
	defer bus.Close()

	sub := bus.Subscribe(nil)
	defer sub.Unsubscribe()

	var count int64
	// Publish 5 events without reading
	for i := 0; i < 5; i++ {
		evt, _ := protocol.NewSyncStateEvent("acme", "synced", "ok", int64(i))
		bus.Publish(evt)
		atomic.AddInt64(&count, 1)
	}

	if count != 5 {
		t.Errorf("expected all 5 events to publish without blocking, got %d", count)
	}
}
