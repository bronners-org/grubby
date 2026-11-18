package ui

import (
	"os"
	"testing"
	"time"

	"grubby/internal/github"
)

func TestSendNativeNotification(t *testing.T) {
	// Verify that SendNativeNotification executes without crashing or throwing errors
	SendNativeNotification("Grubby Test", "Unit Test", "Verification of notification banner")
	time.Sleep(100 * time.Millisecond)
}

func TestSendPRNotification(t *testing.T) {
	pr := github.PullRequest{
		Number:         99,
		Repo:           "test-service",
		RepoFullName:   "my-org/test-service",
		Title:          "Feat: add sticky notifications",
		Author:         "alice",
		ReviewDecision: "APPROVED",
		URL:            "https://github.com/my-org/test-service/pull/99",
	}

	// Verify that SendPRNotification runs without panic or error
	SendPRNotification(pr)
	time.Sleep(100 * time.Millisecond)
}

func TestEnsureGrubbyNotifierApp(t *testing.T) {
	binPath := EnsureGrubbyNotifierApp()
	if binPath == "" {
		t.Skip("swiftc or macOS developer tools not available to build helper")
	}
	if info, err := os.Stat(binPath); err != nil || info.Mode()&0111 == 0 {
		t.Fatalf("expected executable GrubbyNotifier binary at %s, got err: %v", binPath, err)
	}
}
