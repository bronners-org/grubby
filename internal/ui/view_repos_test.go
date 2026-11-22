package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"grubby/internal/config"
	"grubby/internal/git"
	"grubby/internal/github"
	"grubby/internal/storage"
)

func createTestModel() Model {
	cfg := &config.Config{
		Version:      1,
		WorkspaceDir: "~/workspaces",
		Groups: []config.GroupConfig{
			{Name: "backend", DisplayName: "Backend Services", Repos: []string{"auth-service", "api-gateway"}},
			{Name: "frontend", DisplayName: "Frontend Web", Repos: []string{"web-client"}},
		},
	}

	repos := []github.Repository{
		{Name: "auth-service", FullName: "acme/auth-service"},
		{Name: "api-gateway", FullName: "acme/api-gateway"},
		{Name: "web-client", FullName: "acme/web-client"},
		{Name: "docs", FullName: "acme/docs"},
	}

	secSummaries := map[string]github.SecurityAlertSummary{
		"auth-service": {Total: 2, Critical: 1, High: 1},
		"api-gateway":  {Total: 8, Critical: 3, High: 2, Medium: 3},
		"web-client":   {Total: 1, Critical: 0, High: 0, Medium: 1},
		"docs":         {Total: 0},
	}

	rfi := textinput.New()
	rfi.Placeholder = "Type repository name substring..."
	rfi.Prompt = "🔍 "

	return Model{
		ActiveTab:         TabRepos,
		Config:            cfg,
		Repos:             repos,
		SecuritySummaries: secSummaries,
		GitStatuses:       make(map[string]git.RepoGitStatus),
		RepoFilterInput:   rfi,
		SplashDismissed:   true,
	}
}

func TestGetDisplayRepos_NameFilter(t *testing.T) {
	m := createTestModel()

	// Default: all 4 repos
	display := m.getDisplayRepos()
	if len(display) != 4 {
		t.Fatalf("expected 4 repos, got %d", len(display))
	}

	// Filter by substring "api"
	m.RepoFilter = "api"
	display = m.getDisplayRepos()
	if len(display) != 1 || display[0].Name != "api-gateway" {
		t.Fatalf("expected api-gateway, got %+v", display)
	}

	// Filter case-insensitive "AUTH"
	m.RepoFilter = "AUTH"
	display = m.getDisplayRepos()
	if len(display) != 1 || display[0].Name != "auth-service" {
		t.Fatalf("expected auth-service, got %+v", display)
	}

	// Clear filter
	m.RepoFilter = ""
	display = m.getDisplayRepos()
	if len(display) != 4 {
		t.Fatalf("expected 4 repos after clear, got %d", len(display))
	}
}

func TestGetDisplayRepos_GroupFilter(t *testing.T) {
	m := createTestModel()

	// Filter by group name "backend" via unified filter (auth-service, api-gateway)
	m.RepoFilter = "backend"
	display := m.getDisplayRepos()
	if len(display) != 2 {
		t.Fatalf("expected 2 backend repos, got %d", len(display))
	}

	// Filter by group name "frontend" via unified filter (web-client)
	m.RepoFilter = "frontend"
	display = m.getDisplayRepos()
	if len(display) != 1 || display[0].Name != "web-client" {
		t.Fatalf("expected web-client, got %+v", display)
	}

	// Filter by repo name "auth" via unified filter
	m.RepoFilter = "auth"
	display = m.getDisplayRepos()
	if len(display) != 1 || display[0].Name != "auth-service" {
		t.Fatalf("expected auth-service for auth filter, got %+v", display)
	}
}

func TestGetDisplayRepos_SortByVulnerabilities(t *testing.T) {
	m := createTestModel()

	m.RepoSortVulnerabilities = true
	display := m.getDisplayRepos()

	// Order should be:
	// 1. api-gateway (Total: 8, Crit: 3)
	// 2. auth-service (Total: 2, Crit: 1)
	// 3. web-client (Total: 1, Crit: 0)
	// 4. docs (Total: 0)
	if len(display) != 4 {
		t.Fatalf("expected 4 repos, got %d", len(display))
	}

	if display[0].Name != "api-gateway" {
		t.Errorf("expected #1 api-gateway, got %s", display[0].Name)
	}
	if display[1].Name != "auth-service" {
		t.Errorf("expected #2 auth-service, got %s", display[1].Name)
	}
	if display[2].Name != "web-client" {
		t.Errorf("expected #3 web-client, got %s", display[2].Name)
	}
	if display[3].Name != "docs" {
		t.Errorf("expected #4 docs, got %s", display[3].Name)
	}
}

func TestRepoClonedMsg_ImmediateDetection(t *testing.T) {
	m := createTestModel()

	// Initially repo is not cloned
	if st, ok := m.GitStatuses["auth-service"]; ok && st.IsCloned {
		t.Fatalf("expected auth-service to not be cloned initially")
	}

	// Simulate RepoClonedMsg
	msg := RepoClonedMsg{
		RepoName:  "auth-service",
		TargetDir: "~/workspaces/auth-service",
		Status: git.RepoGitStatus{
			RepoName:  "auth-service",
			LocalPath: "~/workspaces/auth-service",
			IsCloned:  true,
			Branch:    "main",
			State:     git.StateSynced,
		},
		Err: nil,
	}

	updatedModel, _ := m.Update(msg)
	mod := updatedModel.(Model)

	st, found := mod.GitStatuses["auth-service"]
	if !found || !st.IsCloned {
		t.Fatalf("expected auth-service to be marked cloned immediately, got: %+v", st)
	}
	if st.Branch != "main" {
		t.Errorf("expected branch main, got %s", st.Branch)
	}
	if st.State != git.StateSynced {
		t.Errorf("expected StateSynced, got %v", st.State)
	}
}

func TestReposKeyHandling_FilterAndSortKeys(t *testing.T) {
	m := createTestModel()

	// Press '/' -> opens ModalFilterRepoName
	updated, _ := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(Model)
	if m.ActiveModal != ModalFilterRepoName {
		t.Errorf("expected ModalFilterRepoName, got %v", m.ActiveModal)
	}

	// Press 'esc' inside modal -> closes modal
	updated, _ = m.handleFilterNameModalKeys(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.ActiveModal != ModalNone {
		t.Errorf("expected ModalNone after esc, got %v", m.ActiveModal)
	}

	// Press '/' again, type query and press enter inside filter modal
	m.RepoFilterInput.SetValue("gateway")
	updated, _ = m.handleFilterNameModalKeys(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.ActiveModal != ModalNone {
		t.Errorf("expected ModalNone after enter, got %v", m.ActiveModal)
	}
	if m.RepoFilter != "gateway" {
		t.Errorf("expected RepoFilter 'gateway', got %q", m.RepoFilter)
	}
	if len(m.getDisplayRepos()) != 1 {
		t.Errorf("expected 1 match for 'gateway', got %d", len(m.getDisplayRepos()))
	}

	// Press 'esc' on repos tab -> clears filter
	updated, _ = m.handleReposKeys(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.RepoFilter != "" {
		t.Errorf("expected cleared RepoFilter, got %q", m.RepoFilter)
	}

	// Filter by group name "backend"
	m.RepoFilterInput.SetValue("backend")
	updated, _ = m.handleFilterNameModalKeys(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if len(m.getDisplayRepos()) != 2 {
		t.Errorf("expected 2 matches for group 'backend', got %d", len(m.getDisplayRepos()))
	}

	// Press 'esc' again -> clears
	updated, _ = m.handleReposKeys(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)

	// Press 'v' -> toggles sort by vulnerabilities
	updated, _ = m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	m = updated.(Model)
	if !m.RepoSortVulnerabilities {
		t.Errorf("expected RepoSortVulnerabilities=true")
	}

	// Press 'v' again -> toggles off
	updated, _ = m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	m = updated.(Model)
	if m.RepoSortVulnerabilities {
		t.Errorf("expected RepoSortVulnerabilities=false")
	}

	// Test uncloned repo auto-clone triggers
	// docs is not cloned in createTestModel()
	m.RepoCursor = 3 // docs
	var cmd tea.Cmd
	updated, _ = m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	updated, cmd = updated.(Model).handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(Model)
	if cmd == nil {
		t.Errorf("expected non-nil clone command when pressing 'o' then 's' on uncloned repo")
	}
	if !strings.Contains(m.StatusMsg, "Cloning") {
		t.Errorf("expected status msg to indicate cloning, got %q", m.StatusMsg)
	}

	updated, _ = m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	updated, cmd = updated.(Model).handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = updated.(Model)
	if cmd == nil {
		t.Errorf("expected non-nil clone command when pressing 'o' then 'e' on uncloned repo")
	}

	updated, cmd = m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = updated.(Model)
	if cmd == nil {
		t.Errorf("expected non-nil clone command when pressing 'c' on uncloned repo")
	}
}

func TestRenderReposView_ColumnsOptimized(t *testing.T) {
	m := createTestModel()
	m.Width = 120
	m.Height = 25

	rendered := m.renderReposView()

	// Verify TMUX and LAST PUSHED columns are removed, LOCAL PATH is present
	if strings.Contains(rendered, "TMUX") {
		t.Errorf("expected rendered repos view to drop separate 'TMUX' header column, got:\n%s", rendered)
	}
	if strings.Contains(rendered, "LAST PUSHED") {
		t.Errorf("expected rendered repos view to drop separate 'LAST PUSHED' header column, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "LOCAL PATH") {
		t.Errorf("expected rendered repos view to contain 'LOCAL PATH' header column, got:\n%s", rendered)
	}
}

func TestRenderReposView_LocalPathWithLastPushedAndTmux(t *testing.T) {
	m := createTestModel()
	m.Width = 160
	m.Height = 30
	now := time.Now()

	m.Repos = []github.Repository{
		{Name: "tasks", FullName: "bronners-org/tasks", PushedAt: now.Add(-5 * 24 * time.Hour)},
		{Name: "web-client", FullName: "bronners-org/web-client", PushedAt: now.Add(-1 * 24 * time.Hour)},
	}
	m.GitStatuses = map[string]git.RepoGitStatus{
		"tasks": {
			RepoName:    "tasks",
			LocalPath:   "/Users/test/workspaces/tasks",
			IsCloned:    true,
			Branch:      "main",
			State:       git.StateSynced,
			HasUpstream: true,
		},
		"web-client": {
			RepoName:    "web-client",
			LocalPath:   "/Users/test/workspaces/web-client",
			IsCloned:    true,
			Branch:      "main",
			State:       git.StateSynced,
			HasUpstream: true,
		},
	}
	m.TmuxSessions = map[string]TmuxSessionInfo{
		"tasks": {
			Windows: 1,
		},
	}

	rendered := m.renderReposView()

	// Verify tasks row contains [5d] and (tmux: 1)
	if !strings.Contains(rendered, "[5d]") {
		t.Errorf("expected rendered view to contain '[5d]', got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "(tmux: 1)") {
		t.Errorf("expected rendered view to contain '(tmux: 1)', got:\n%s", rendered)
	}

	// Verify web-client row contains [1d] and does NOT contain tmux indicator
	if !strings.Contains(rendered, "[1d]") {
		t.Errorf("expected rendered view to contain '[1d]', got:\n%s", rendered)
	}
}

func TestFormatTimeAgo_CompactWithoutAgo(t *testing.T) {
	now := time.Now()

	cases := []struct {
		name     string
		t        time.Time
		expected string
	}{
		{"zero", time.Time{}, "-"},
		{"just now (30s)", now.Add(-30 * time.Second), "just now"},
		{"minutes (1m)", now.Add(-1 * time.Minute), "1m"},
		{"minutes (45m)", now.Add(-45 * time.Minute), "45m"},
		{"hours (1h)", now.Add(-1 * time.Hour), "1h"},
		{"hours (12h)", now.Add(-12 * time.Hour), "12h"},
		{"days (1d)", now.Add(-24 * time.Hour), "1d"},
		{"days (3d)", now.Add(-3 * 24 * time.Hour), "3d"},
		{"weeks (2w)", now.Add(-14 * 24 * time.Hour), "2w"},
		{"months (2mo)", now.Add(-60 * 24 * time.Hour), "2mo"},
		{"years (1y)", now.Add(-400 * 24 * time.Hour), "1y"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatTimeAgo(tc.t)
			if got != tc.expected {
				t.Errorf("formatTimeAgo(%s) = %q, want %q", tc.name, got, tc.expected)
			}
		})
	}
}

func TestRenderReposView_GitStatusRealEstateAndSeparation(t *testing.T) {
	m := createTestModel()
	m.Width = 120
	m.Height = 25
	now := time.Now()

	m.Repos = []github.Repository{
		{
			Name:     "grubby",
			FullName: "bronners-org/grubby",
			PushedAt: now.Add(-3 * 24 * time.Hour),
		},
	}
	m.GitStatuses = map[string]git.RepoGitStatus{
		"grubby": {
			RepoName:    "grubby",
			LocalPath:   "/Users/test/workspaces/grubby",
			IsCloned:    true,
			Branch:      "hide-notifications-for-hidden",
			IsDirty:     true,
			State:       git.StateDirty,
			HasUpstream: true,
		},
	}

	rendered := m.renderReposView()

	// Verify that the branch name has more real-estate than the old 16-col truncation ("* hide-notifica…")
	// With GitWidth=24 (badge width 22), it shows "* hide-notifications-…"
	if !strings.Contains(rendered, "* hide-notifications-…") {
		t.Errorf("expected rendered view to show expanded git status '* hide-notifications-…', got:\n%s", rendered)
	}

	// Verify [3d] is present
	if !strings.Contains(rendered, "[3d]") {
		t.Errorf("expected rendered view to contain '[3d]', got:\n%s", rendered)
	}

	// Crucial check: Git status and [3d] MUST NOT directly touch each other
	if strings.Contains(rendered, "…[3d]") {
		t.Errorf("CRITICAL: git status and [3d] are colliding without whitespace separation, got:\n%s", rendered)
	}
}

func TestRenderReposView_TmuxSessionAlwaysVisibleOnLongPath(t *testing.T) {
	m := createTestModel()
	m.Width = 90
	m.Height = 25
	now := time.Now()

	m.Repos = []github.Repository{
		{Name: "very-long-repo-name-service-backend", FullName: "org/very-long-repo-name-service-backend", PushedAt: now.Add(-2 * time.Hour)},
	}
	m.GitStatuses = map[string]git.RepoGitStatus{
		"very-long-repo-name-service-backend": {
			RepoName:  "very-long-repo-name-service-backend",
			LocalPath: "/Users/test/workspaces/corporate-org/very-long-repo-name-service-backend",
			IsCloned:  true,
		},
	}
	m.TmuxSessions = map[string]TmuxSessionInfo{
		"very-long-repo-name-service-backend": {
			Windows: 2,
		},
	}

	rendered := m.renderReposView()
	if !strings.Contains(rendered, "(tmux: 2)") {
		t.Errorf("expected '(tmux: 2)' to remain visible despite long path and constrained width, got:\n%s", rendered)
	}
}

func TestGetDisplayRepos_SortByInteractedThenUpdated(t *testing.T) {
	now := time.Now()

	cfg := &config.Config{
		Version: 1,
		RepoInteractions: map[string]time.Time{
			"repo-interacted-old": now.Add(-2 * time.Hour),
			"repo-interacted-new": now.Add(-10 * time.Minute),
		},
	}

	repos := []github.Repository{
		{Name: "repo-never-touched-old", PushedAt: now.Add(-5 * 24 * time.Hour)},
		{Name: "repo-never-touched-new", PushedAt: now.Add(-1 * time.Hour)},
		{Name: "repo-interacted-old", PushedAt: now.Add(-10 * 24 * time.Hour)},
		{Name: "repo-interacted-new", PushedAt: now.Add(-12 * 24 * time.Hour)},
		{Name: "repo-tmux-session", PushedAt: now.Add(-20 * 24 * time.Hour)},
	}

	// repo-tmux-session has an active tmux session with latest activity 5 mins ago
	tmuxSessions := map[string]TmuxSessionInfo{
		"repo-tmux-session": {
			Windows:      1,
			LastActivity: now.Add(-5 * time.Minute),
		},
	}

	m := Model{
		Config:       cfg,
		Repos:        repos,
		TmuxSessions: tmuxSessions,
		GitStatuses:  make(map[string]git.RepoGitStatus),
	}

	display := m.getDisplayRepos()
	if len(display) != 5 {
		t.Fatalf("expected 5 repos, got %d", len(display))
	}

	// Expected order:
	// 1. repo-tmux-session (interacted via tmux 5m ago)
	// 2. repo-interacted-new (interacted 10m ago)
	// 3. repo-interacted-old (interacted 2h ago)
	// 4. repo-never-touched-new (updated 1h ago)
	// 5. repo-never-touched-old (updated 5d ago)
	expected := []string{
		"repo-tmux-session",
		"repo-interacted-new",
		"repo-interacted-old",
		"repo-never-touched-new",
		"repo-never-touched-old",
	}

	for i, exp := range expected {
		if display[i].Name != exp {
			t.Errorf("pos %d: expected %s, got %s", i, exp, display[i].Name)
		}
	}
}

func TestReposKeyHandling_RecordInteractionMovesToTop(t *testing.T) {
	now := time.Now()

	cfg := &config.Config{
		Version: 1,
		RepoInteractions: map[string]time.Time{
			"repo-a": now.Add(-10 * time.Minute),
		},
	}

	repos := []github.Repository{
		{Name: "repo-a", PushedAt: now.Add(-2 * time.Hour)},
		{Name: "repo-b", PushedAt: now.Add(-1 * time.Hour)},
	}

	m := Model{
		Config:       cfg,
		Repos:        repos,
		TmuxSessions: make(map[string]TmuxSessionInfo),
		GitStatuses: map[string]git.RepoGitStatus{
			"repo-b": {RepoName: "repo-b", IsCloned: true, LocalPath: "/tmp/repo-b"},
		},
	}

	// Initially repo-a is at index 0 because it was interacted with 10m ago
	display := m.getDisplayRepos()
	if display[0].Name != "repo-a" {
		t.Fatalf("expected repo-a at pos 0 initially, got %s", display[0].Name)
	}

	// Cursor is on repo-b, user presses 'o' then 'w' to open web
	for idx, it := range m.getRepoTableItems() {
		if it.Kind == ItemRepo && it.Repo != nil && it.Repo.Name == "repo-b" {
			m.RepoCursor = idx
			break
		}
	}
	updated, _ := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	updated, _ = updated.(Model).handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	m = updated.(Model)

	// repo-b was interacted with just now, so it should move to pos 0 in getDisplayRepos()
	newDisplay := m.getDisplayRepos()
	if newDisplay[0].Name != "repo-b" {
		t.Errorf("expected repo-b to move to pos 0 after interaction, got %s", newDisplay[0].Name)
	}
	// And cursor follows repo-b in table items
	items := m.getRepoTableItems()
	if m.RepoCursor < len(items) && (items[m.RepoCursor].Repo == nil || items[m.RepoCursor].Repo.Name != "repo-b") {
		t.Errorf("expected cursor to follow repo-b, got item: %+v", items[m.RepoCursor])
	}
}

func TestRepoTiersRenderingAndSorting(t *testing.T) {
	now := time.Now()
	repos := []github.Repository{
		{Name: "dead-repo", PushedAt: now.Add(-400 * 24 * time.Hour)},
		{Name: "cold-repo", PushedAt: now.Add(-60 * 24 * time.Hour)},
		{Name: "warm-repo", PushedAt: now.Add(-10 * 24 * time.Hour)},
		{Name: "hot-repo", PushedAt: now.Add(-1 * time.Hour)},
	}

	tiers := map[string]storage.RepoTier{
		"dead-repo": storage.TierDead,
		"cold-repo": storage.TierCold,
		"warm-repo": storage.TierWarm,
		"hot-repo":  storage.TierHot,
	}

	m := Model{
		Repos:        repos,
		RepoTiers:    tiers,
		GitStatuses:  make(map[string]git.RepoGitStatus),
		Width:        140,
		Height:       30,
		TmuxSessions: make(map[string]TmuxSessionInfo),
	}

	// 1. Verify sorting: Hot comes first, then Warm, then Cold, then Dead
	sorted := m.getDisplayRepos()
	if len(sorted) != 4 {
		t.Fatalf("expected 4 repos, got %d", len(sorted))
	}
	if sorted[0].Name != "hot-repo" {
		t.Errorf("expected hot-repo first, got %s", sorted[0].Name)
	}
	if sorted[1].Name != "warm-repo" {
		t.Errorf("expected warm-repo second, got %s", sorted[1].Name)
	}
	if sorted[2].Name != "cold-repo" {
		t.Errorf("expected cold-repo third, got %s", sorted[2].Name)
	}
	if sorted[3].Name != "dead-repo" {
		t.Errorf("expected dead-repo fourth, got %s", sorted[3].Name)
	}

	// 2. Verify rendering: tier headers as tree nodes, and TIER column is removed
	view := m.renderReposView()
	if strings.Contains(view, "  TIER  ") {
		t.Errorf("expected 'TIER' header column to be removed, got:\n%s", view)
	}
	if !strings.Contains(view, "▼ 🔥 HOT") || !strings.Contains(view, "▼ 🌞 WARM") || !strings.Contains(view, "▼ 🧊 COLD") || !strings.Contains(view, "▶ 💀 DEAD") {
		t.Errorf("expected tier tree nodes, got:\n%s", view)
	}
	// Dead tier is collapsed by default: dead-repo should not be visible
	if strings.Contains(view, "dead-repo") {
		t.Errorf("expected dead-repo to be hidden when Dead tier is collapsed, got:\n%s", view)
	}
	// Space to expand hint shown on collapsed Dead tier
	if !strings.Contains(view, "[Space to expand]") {
		t.Errorf("expected '[Space to expand]' hint on collapsed Dead tier, got:\n%s", view)
	}

	// 3. Expand Dead tier -> dead-repo should appear
	m.CollapsedTiers = map[storage.RepoTier]bool{storage.TierDead: false}
	viewExpanded := m.renderReposView()
	if !strings.Contains(viewExpanded, "dead-repo") {
		t.Errorf("expected dead-repo to be visible after expanding Dead tier, got:\n%s", viewExpanded)
	}

	// 4. Verify tier count summary banner
	if !strings.Contains(view, "🔥 1 Hot") || !strings.Contains(view, "🌞 1 Warm") || !strings.Contains(view, "🧊 1 Cold") || !strings.Contains(view, "💀 1 Dead") {
		t.Errorf("expected tier count summary in header banner, got:\n%s", view)
	}
}

func TestRepoOpenPRs_InlineExpansion(t *testing.T) {
	repos := []github.Repository{
		{Name: "my-service", FullName: "my-org/my-service"},
		{Name: "other-repo", FullName: "my-org/other-repo"},
	}
	prs := []github.PullRequest{
		{
			Number:         42,
			Repo:           "my-service",
			RepoFullName:   "my-org/my-service",
			Title:          "Fix database deadlock",
			Author:         "alice",
			URL:            "https://github.com/my-org/my-service/pull/42",
			ReviewDecision: "APPROVED",
		},
	}
	m := Model{
		ActiveTab:       TabRepos,
		Repos:           repos,
		PRs:             prs,
		Width:           140,
		Height:          30,
		ExpandedRepoPRs: make(map[string]bool),
	}
	for idx, it := range m.getRepoTableItems() {
		if it.Kind == ItemRepo && it.Repo != nil && it.Repo.Name == "my-service" {
			m.RepoCursor = idx
			break
		}
	}

	// Press Enter on my-service -> toggles PR expansion
	updated, _ := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyEnter})
	upModel := updated.(Model)

	if !upModel.isRepoPRExpanded(&repos[0]) {
		t.Fatalf("expected my-service PRs to be expanded")
	}

	items := upModel.getRepoTableItems()
	foundPR := false
	var prIdx int
	for idx, it := range items {
		if it.Kind == ItemRepoPR && it.PR != nil && it.PR.Number == 42 {
			foundPR = true
			prIdx = idx
			break
		}
	}
	if !foundPR {
		t.Fatalf("expected ItemRepoPR child item in table items")
	}

	// Move cursor to PR item
	upModel.RepoCursor = prIdx
	updatedPR, cmdPR := upModel.handleReposKeys(tea.KeyMsg{Type: tea.KeyEnter})
	if cmdPR == nil {
		t.Fatalf("expected toast message when opening PR in browser")
	}
	toastMsg := string(cmdPR().(StatusToastMsg))
	if !strings.Contains(toastMsg, "PR #42") {
		t.Errorf("expected toast to mention PR #42, got %q", toastMsg)
	}

	// Press 'y' on PR item -> yanks PR URL
	_, cmdYank := updatedPR.(Model).handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if cmdYank == nil {
		t.Fatalf("expected toast message when yanking PR URL")
	}
	yankMsg := string(cmdYank().(StatusToastMsg))
	if !strings.Contains(yankMsg, "Copied PR #42 URL") {
		t.Errorf("expected toast to mention copied URL, got %q", yankMsg)
	}
}

func TestRepoInteractionTouchesSQLite(t *testing.T) {
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	now := time.Now()
	repos := []github.Repository{
		{Name: "my-cold-repo", FullName: "org/my-cold-repo", PushedAt: now.Add(-60 * 24 * time.Hour)},
	}
	if err := db.SaveRepositories(repos, "org"); err != nil {
		t.Fatalf("SaveRepositories failed: %v", err)
	}
	_ = db.SetRepoTier("my-cold-repo", storage.TierCold)

	m := Model{
		DB:           db,
		Repos:        repos,
		RepoTiers:    map[string]storage.RepoTier{"my-cold-repo": storage.TierCold},
		GitStatuses:  make(map[string]git.RepoGitStatus),
		Width:        120,
		Height:       30,
		TmuxSessions: make(map[string]TmuxSessionInfo),
	}

	if tier := m.getRepoTier("my-cold-repo"); tier != storage.TierCold {
		t.Fatalf("expected initial tier cold, got %v", tier)
	}

	// User interacts with repo (open via 'o' then 'w' on my-cold-repo)
	for idx, it := range m.getRepoTableItems() {
		if it.Kind == ItemRepo && it.Repo != nil && it.Repo.Name == "my-cold-repo" {
			m.RepoCursor = idx
			break
		}
	}
	updated, _ := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	updated, _ = updated.(Model).handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	m = updated.(Model)

	// Interaction must NOT change tier (it stays TierCold!)
	if tier := m.getRepoTier("my-cold-repo"); tier != storage.TierCold {
		t.Errorf("expected in-memory tier to remain TierCold, got %v", tier)
	}
	if dbTier := db.GetRepoTier("my-cold-repo"); dbTier != storage.TierCold {
		t.Errorf("expected SQLite db tier to remain TierCold, got %v", dbTier)
	}

	// SQLite database records last_interacted_at
	interacted := db.GetRepoLastInteractedAt("my-cold-repo")
	if interacted.IsZero() || time.Since(interacted) > 5*time.Second {
		t.Errorf("expected valid recent last_interacted_at, got %v", interacted)
	}
}

func TestRepoInteractionPushesToTopOfTier(t *testing.T) {
	now := time.Now()
	repos := []github.Repository{
		{Name: "hot-1", PushedAt: now.Add(-1 * time.Hour)},
		{Name: "cold-a", PushedAt: now.Add(-50 * 24 * time.Hour)},
		{Name: "cold-b", PushedAt: now.Add(-45 * 24 * time.Hour)}, // pushed more recently than cold-a
		{Name: "dead-1", PushedAt: now.Add(-500 * 24 * time.Hour)},
	}

	tiers := map[string]storage.RepoTier{
		"hot-1":  storage.TierHot,
		"cold-a": storage.TierCold,
		"cold-b": storage.TierCold,
		"dead-1": storage.TierDead,
	}

	m := Model{
		Repos:        repos,
		RepoTiers:    tiers,
		GitStatuses:  make(map[string]git.RepoGitStatus),
		Width:        140,
		Height:       30,
		TmuxSessions: make(map[string]TmuxSessionInfo),
	}

	// Initially, cold-b comes before cold-a because it was pushed more recently (45d vs 50d)
	sorted := m.getDisplayRepos()
	if sorted[1].Name != "cold-b" || sorted[2].Name != "cold-a" {
		t.Fatalf("expected [hot-1, cold-b, cold-a, dead-1], got %s, %s, %s, %s",
			sorted[0].Name, sorted[1].Name, sorted[2].Name, sorted[3].Name)
	}

	// User interacts with cold-a (inspecting it)
	m = m.recordRepoInteraction("cold-a")

	// cold-a must still be TierCold!
	if tier := m.getRepoTier("cold-a"); tier != storage.TierCold {
		t.Errorf("expected cold-a to stay TierCold, got %v", tier)
	}

	// Now cold-a must be pushed to the top of the Cold tier (ahead of cold-b, but behind hot-1)!
	sortedAfter := m.getDisplayRepos()
	if sortedAfter[0].Name != "hot-1" {
		t.Errorf("expected hot-1 to remain first, got %s", sortedAfter[0].Name)
	}
	if sortedAfter[1].Name != "cold-a" {
		t.Errorf("expected interacted cold-a to be pushed to top of Cold tier (position 1), got %s", sortedAfter[1].Name)
	}
	if sortedAfter[2].Name != "cold-b" {
		t.Errorf("expected cold-b at position 2, got %s", sortedAfter[2].Name)
	}
	if sortedAfter[3].Name != "dead-1" {
		t.Errorf("expected dead-1 to remain at position 3, got %s", sortedAfter[3].Name)
	}
}

func TestCollapsibleTierTree_TogglingAndAutoExpansion(t *testing.T) {
	now := time.Now()
	repos := []github.Repository{
		{ID: 101, Name: "hot-repo", PushedAt: now.Add(-1 * time.Hour)},
		{ID: 102, Name: "warm-repo", PushedAt: now.Add(-5 * 24 * time.Hour)},
		{ID: 103, Name: "dead-repo", PushedAt: now.Add(-400 * 24 * time.Hour)},
	}
	tiers := map[string]storage.RepoTier{
		"hot-repo":  storage.TierHot,
		"warm-repo": storage.TierWarm,
		"dead-repo": storage.TierDead,
	}

	m := Model{
		Repos:           repos,
		RepoTiers:       tiers,
		GitStatuses:     make(map[string]git.RepoGitStatus),
		Width:           120,
		Height:          30,
		SplashDismissed: true,
		CollapsedTiers:  map[storage.RepoTier]bool{storage.TierDead: true},
	}

	// 1. Initial State:
	// Hot tier (Header + hot-repo)
	// Warm tier (Header + warm-repo)
	// Dead tier (Header only, collapsed)
	items := m.getRepoTableItems()
	if len(items) != 5 {
		t.Fatalf("expected 5 table items (Hot Hdr + Repo, Warm Hdr + Repo, Dead Hdr), got %d", len(items))
	}
	if items[4].Kind != ItemTierHeader || items[4].Tier != storage.TierDead || items[4].Expanded {
		t.Errorf("expected item 4 to be collapsed Dead tier header, got: %+v", items[4])
	}

	// 2. Cursor on Dead tier header (index 4), press Space -> expands
	m.RepoCursor = 4
	updated, _ := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m = updated.(Model)
	if m.isTierCollapsed(storage.TierDead) {
		t.Fatalf("expected Dead tier to be expanded after Space")
	}
	itemsAfterExpand := m.getRepoTableItems()
	if len(itemsAfterExpand) != 6 {
		t.Fatalf("expected 6 items after expanding Dead tier (now includes dead-repo), got %d", len(itemsAfterExpand))
	}
	if itemsAfterExpand[5].Kind != ItemRepo || itemsAfterExpand[5].Repo.Name != "dead-repo" {
		t.Errorf("expected item 5 to be dead-repo, got: %+v", itemsAfterExpand[5])
	}

	// 3. Cursor on dead-repo (index 5), press '[' -> collapses Dead tier and lands cursor back on Dead header
	m.RepoCursor = 5
	updated, _ = m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'['}})
	m = updated.(Model)
	if !m.isTierCollapsed(storage.TierDead) {
		t.Fatalf("expected Dead tier to be collapsed after pressing '[' on child repo")
	}
	if m.RepoCursor != 4 {
		t.Errorf("expected cursor to jump to Dead header at index 4, got %d", m.RepoCursor)
	}

	// 4. Auto-expansion during search ('/' or '#')
	// Filter by "dead"
	m.RepoFilter = "dead"
	if m.isTierCollapsed(storage.TierDead) {
		t.Errorf("expected Dead tier to auto-expand when search query is active")
	}
	autoExpandedItems := m.getRepoTableItems()
	// Only Dead tier matches "dead" -> Dead Header + dead-repo = 2 items
	if len(autoExpandedItems) != 2 {
		t.Fatalf("expected 2 items for 'dead' query (Header + dead-repo), got %d", len(autoExpandedItems))
	}

	// 5. Esc clears search and restores collapsed state
	updated, _ = m.handleReposKeys(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.RepoFilter != "" {
		t.Errorf("expected RepoFilter to be cleared, got %q", m.RepoFilter)
	}
	if !m.isTierCollapsed(storage.TierDead) {
		t.Errorf("expected Dead tier to be collapsed again after clearing search")
	}
}

func TestSummaryColumnAndInlineLabelsRendering(t *testing.T) {
	now := time.Now()
	repos := []github.Repository{
		{
			ID:          101,
			Name:        "auth-svc",
			Description: "Authentication and authorization service for core APIs",
			PushedAt:    now.Add(-1 * time.Hour),
		},
	}
	tiers := map[string]storage.RepoTier{
		"auth-svc": storage.TierHot,
	}
	secSummaries := map[string]github.SecurityAlertSummary{
		"auth-svc": {Total: 5, Critical: 1, High: 2, Medium: 2},
	}
	prs := []github.PullRequest{
		{Repo: "auth-svc", Number: 10, State: "OPEN"},
		{Repo: "auth-svc", Number: 11, State: "OPEN"},
	}
	personalTasks := []github.Task{
		{RepoName: "auth-svc", Number: 1, State: "open"},
	}
	squadTasks := []github.Task{
		{RepoName: "auth-svc", Number: 2, State: "open"},
		{RepoName: "auth-svc", Number: 3, State: "open"},
	}

	m := Model{
		Repos:             repos,
		RepoTiers:         tiers,
		SecuritySummaries: secSummaries,
		PRs:               prs,
		PersonalTasks:     personalTasks,
		SquadTasks:        squadTasks,
		RepoLabels: map[int64][]string{
			101: {"backend", "core"},
		},
		RepoLabelColors: map[string]string{
			"backend": "1d76db",
			"core":    "5319e7",
		},
		GitStatuses:     make(map[string]git.RepoGitStatus),
		Width:           160,
		Height:          30,
		SplashDismissed: true,
	}

	view := m.renderReposView()

	// 1. Verify column header is (Vulns - Issues)
	if !strings.Contains(view, "(Vulns - Issues)") {
		t.Errorf("expected '(Vulns - Issues)' column header, got:\n%s", view)
	}
	if strings.Contains(view, "(Vulns - PRs - Issues)") {
		t.Errorf("expected '(Vulns - PRs - Issues)' to be updated to '(Vulns - Issues)', got:\n%s", view)
	}

	// 2. Verify inline labels #backend and #core are rendered
	if !strings.Contains(view, "#backend") || !strings.Contains(view, "#core") {
		t.Errorf("expected inline labels #backend and #core in repo view, got:\n%s", view)
	}

	// 3. Verify repository About / description text is rendered (chopped with … if needed)
	if !strings.Contains(view, "Authenticati") {
		t.Errorf("expected repository About text in repo view, got:\n%s", view)
	}

	// 4. Verify (Vulns - Issues) format: ( 01 02 02 ) - ( 3 ) with 2-digit alignment
	if !strings.Contains(view, "( 01 02 02 )") {
		t.Errorf("expected '( 01 02 02 )' vulnerabilities with 2-digit alignment, got:\n%s", view)
	}
	if !strings.Contains(view, "( 3 )") {
		t.Errorf("expected ( 3 ) task count in column, got:\n%s", view)
	}
	// Verify that the summary column does NOT contain the separate ( 2 ) PR badge between vulns and tasks
	if strings.Contains(view, "( 01 02 02 ) - ( 2 ) - ( 3 )") {
		t.Errorf("expected PR count to be removed from (Vulns - Issues) column, got:\n%s", view)
	}
	// Verify that PR count is shown in the REPOSITORY & LABELS column
	if !strings.Contains(view, "[▶ 2 PRs]") {
		t.Errorf("expected '[▶ 2 PRs]' in REPOSITORY & LABELS column, got:\n%s", view)
	}

	// 5. Test clean repo with 0 vulnerabilities renders washed out italic ( clear ✓✓ )
	m.SecuritySummaries["auth-svc"] = github.SecurityAlertSummary{Total: 0}
	viewClean := m.renderReposView()
	if !strings.Contains(viewClean, "( clear ✓✓ )") {
		t.Errorf("expected '( clear ✓✓ )' for 0 vulnerabilities, got:\n%s", viewClean)
	}
	if strings.Contains(viewClean, "( 0 0 0 )") || strings.Contains(viewClean, "( 00 00 00 )") || strings.Contains(viewClean, "👍") {
		t.Errorf("expected 0 vulnerabilities to render as '( clear ✓✓ )', got:\n%s", viewClean)
	}

	// 6. Verify single spinner indicator in top tier bar when Loading is true
	m.Loading = true
	loadingView := m.renderReposView()
	if !strings.Contains(loadingView, "Syncing...") {
		t.Errorf("expected 'Syncing...' in tier bar when Loading is true, got:\n%s", loadingView)
	}
}

func TestVulnColumnWidthAlignment(t *testing.T) {
	// ( clear ✓✓ ) and ( 01 06 05 ) must have identical visual width (12 cells)
	wClean := runewidth.StringWidth("( clear ✓✓ )")
	wVuln := runewidth.StringWidth("( 01 06 05 )")
	if wClean != 12 {
		t.Errorf("expected ( clear ✓✓ ) width to be 12, got %d", wClean)
	}
	if wVuln != 12 {
		t.Errorf("expected ( 01 06 05 ) width to be 12, got %d", wVuln)
	}
	if wClean != wVuln {
		t.Errorf("widths do not match for vertical alignment: %d != %d", wClean, wVuln)
	}
}

func TestHotkeyHashOpensLabelFilterModal(t *testing.T) {
	repos := []github.Repository{
		{ID: 101, Name: "auth-svc"},
	}
	m := Model{
		Repos:           repos,
		Width:           120,
		Height:          30,
		SplashDismissed: true,
	}

	// Press '#' -> opens ModalFilterGroup
	updated, _ := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'#'}})
	m = updated.(Model)
	if m.ActiveModal != ModalFilterGroup {
		t.Errorf("expected ModalFilterGroup when pressing '#', got %v", m.ActiveModal)
	}
}

func TestHotkeyLOpensLabelsModal(t *testing.T) {
	repos := []github.Repository{
		{ID: 101, Name: "auth-svc"},
	}
	m := Model{
		Repos:           repos,
		Width:           120,
		Height:          30,
		SplashDismissed: true,
	}

	// Move cursor to the repo (index 1 under Warm header)
	items := m.getRepoTableItems()
	for idx, it := range items {
		if it.Kind == ItemRepo && it.Repo != nil && it.Repo.Name == "auth-svc" {
			m.RepoCursor = idx
			break
		}
	}

	// Press 'l' -> opens ModalManageGroups (Manage Labels)
	updated, _ := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updated.(Model)
	if m.ActiveModal != ModalManageGroups {
		t.Errorf("expected ModalManageGroups when pressing 'l', got %v", m.ActiveModal)
	}
}

func TestHideAndUnhideRepository(t *testing.T) {
	now := time.Now()
	repos := []github.Repository{
		{ID: 101, Name: "auth-svc", PushedAt: now.Add(-1 * time.Hour)},
		{ID: 102, Name: "core-tasks", PushedAt: now.Add(-10 * time.Minute)},
		{ID: 103, Name: "dead-repo", PushedAt: now.Add(-500 * 24 * time.Hour)},
	}
	tiers := map[string]storage.RepoTier{
		"auth-svc":   storage.TierHot,
		"core-tasks": storage.TierHot,
		"dead-repo":  storage.TierDead,
	}

	m := Model{
		Repos:           repos,
		RepoTiers:       tiers,
		GitStatuses:     make(map[string]git.RepoGitStatus),
		Width:           140,
		Height:          30,
		SplashDismissed: true,
		CollapsedTiers: map[storage.RepoTier]bool{
			storage.TierDead:   true,
			storage.TierHidden: true,
		},
		HiddenRepos: make(map[string]bool),
	}

	// 1. Initially core-tasks is in Hot tier
	if m.getRepoTier("core-tasks") != storage.TierHot {
		t.Fatalf("expected core-tasks to be TierHot initially, got %v", m.getRepoTier("core-tasks"))
	}

	// Select core-tasks in items
	items := m.getRepoTableItems()
	for idx, it := range items {
		if it.Kind == ItemRepo && it.Repo != nil && it.Repo.Name == "core-tasks" {
			m.RepoCursor = idx
			break
		}
	}

	// 2. Press 'h' on core-tasks -> hides it!
	updated, _ := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	m = updated.(Model)

	if !m.isRepoHidden("core-tasks") {
		t.Fatalf("expected core-tasks to be hidden after pressing 'h'")
	}
	if tier := m.getRepoTier("core-tasks"); tier != storage.TierHidden {
		t.Fatalf("expected core-tasks tier to become TierHidden, got %v", tier)
	}

	// 3. Verify Hidden section is underneath Dead tier and collapsed by default
	newItems := m.getRepoTableItems()
	foundDeadHeader := false
	foundHiddenHeader := false
	for _, it := range newItems {
		if it.Kind == ItemTierHeader {
			if it.Tier == storage.TierDead {
				foundDeadHeader = true
			}
			if it.Tier == storage.TierHidden {
				if !foundDeadHeader {
					t.Errorf("expected Hidden tier header to appear AFTER Dead tier header")
				}
				foundHiddenHeader = true
				if it.Expanded {
					t.Errorf("expected Hidden tier to be collapsed by default")
				}
			}
		}
		if it.Kind == ItemRepo && it.Repo.Name == "core-tasks" {
			t.Errorf("expected hidden repo core-tasks to be hidden when Hidden tier is collapsed")
		}
	}
	if !foundHiddenHeader {
		t.Fatalf("expected Hidden tier header in table items")
	}

	// 4. Verify View shows Hidden tier header
	view := m.renderReposView()
	if !strings.Contains(view, "🙈 HIDDEN") {
		t.Errorf("expected '🙈 HIDDEN' section header in view, got:\n%s", view)
	}
	if !strings.Contains(view, "1 Hidden") {
		t.Errorf("expected '1 Hidden' in tier summary bar, got:\n%s", view)
	}

	// 5. Expand Hidden tier -> core-tasks is visible with dimmed style
	m.CollapsedTiers[storage.TierHidden] = false
	viewExpanded := m.renderReposView()
	if !strings.Contains(viewExpanded, "core-tasks") {
		t.Errorf("expected core-tasks to be visible after expanding Hidden tier, got:\n%s", viewExpanded)
	}

	// 6. Press 'h' on core-tasks again -> unhides it!
	for idx, it := range m.getRepoTableItems() {
		if it.Kind == ItemRepo && it.Repo != nil && it.Repo.Name == "core-tasks" {
			m.RepoCursor = idx
			break
		}
	}
	updated, _ = m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	m = updated.(Model)

	if m.isRepoHidden("core-tasks") {
		t.Fatalf("expected core-tasks to be unhidden after pressing 'h' again")
	}
	if tier := m.getRepoTier("core-tasks"); tier != storage.TierHot {
		t.Fatalf("expected core-tasks tier to restore to TierHot, got %v", tier)
	}
}

func TestReposView_SquadOwnershipToggle_ShiftO(t *testing.T) {
	m := createTestModel()
	m.Config.ActiveSquad = "backend-squad"
	m.Config.Squads = []config.SquadConfig{
		{
			Name:        "backend-squad",
			DisplayName: "Backend Squad",
			Members:     []string{"alice"},
			Repos:       []string{},
		},
	}

	// Select first repo: auth-service
	items := m.getRepoTableItems()
	var authIdx int = -1
	for idx, it := range items {
		if it.Kind == ItemRepo && it.Repo != nil && it.Repo.Name == "auth-service" {
			authIdx = idx
			break
		}
	}
	if authIdx == -1 {
		t.Fatalf("auth-service not found in repo table items")
	}
	m.RepoCursor = authIdx

	if m.Config.IsRepoOwnedBySquad("backend-squad", "auth-service") {
		t.Fatalf("expected auth-service not to be owned initially")
	}

	// Press 'O' (Shift-O)
	updated, _ := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'O'}})
	m = updated.(Model)

	if !m.Config.IsRepoOwnedBySquad("backend-squad", "auth-service") {
		t.Fatalf("expected auth-service to be owned by backend-squad after Shift-O")
	}

	// Check that view contains ownership icon 👥 and the repo name
	view := m.renderReposView()
	if !strings.Contains(view, "👥") || !strings.Contains(view, "auth-service") {
		t.Fatalf("expected view to contain '👥' and 'auth-service', got:\n%s", view)
	}

	// Press 'O' again to toggle off
	updated, _ = m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'O'}})
	m = updated.(Model)

	if m.Config.IsRepoOwnedBySquad("backend-squad", "auth-service") {
		t.Fatalf("expected auth-service ownership to be removed after pressing Shift-O again")
	}
}

func TestReposView_SquadFilterToggle_ShiftS(t *testing.T) {
	m := createTestModel()
	m.Config.ActiveSquad = "backend-squad"
	m.Config.Squads = []config.SquadConfig{
		{
			Name:        "backend-squad",
			DisplayName: "Backend Squad",
			Members:     []string{"alice"},
			Repos:       []string{"auth-service"},
		},
	}

	if m.SquadFilterActive {
		t.Fatalf("expected squad filter to be inactive initially")
	}

	// Initially all 4 repos are in display list
	if len(m.getDisplayRepos()) != 4 {
		t.Fatalf("expected 4 display repos initially, got %d", len(m.getDisplayRepos()))
	}

	// Press 'f' (Filter leader), then 's' (squad filter toggle)
	updated, _ := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = updated.(Model)
	if m.RepoLeaderMode != RepoLeaderFilter {
		t.Fatalf("expected RepoLeaderFilter, got %v", m.RepoLeaderMode)
	}
	updated, _ = m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(Model)

	if !m.SquadFilterActive {
		t.Fatalf("expected SquadFilterActive to be true after pressing 'S'")
	}

	display := m.getDisplayRepos()
	if len(display) != 1 || display[0].Name != "auth-service" {
		t.Fatalf("expected only auth-service in display repos when squad filter active, got %+v", display)
	}

	view := m.renderReposView()
	if !strings.Contains(view, "Squad: Backend Squad") {
		t.Fatalf("expected view to display 'Squad: Backend Squad' filter badge, got:\n%s", view)
	}

	// Press 'esc' to clear squad filter
	updated, _ = m.handleReposKeys(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)

	if m.SquadFilterActive {
		t.Fatalf("expected SquadFilterActive to be false after pressing 'esc'")
	}
	if len(m.getDisplayRepos()) != 4 {
		t.Fatalf("expected 4 repos restored after clearing squad filter, got %d", len(m.getDisplayRepos()))
	}
}

func TestTierCollapsingDefaultsAndShortcuts(t *testing.T) {
	now := time.Now()
	repos := []github.Repository{
		{Name: "hot-repo", PushedAt: now.Add(-1 * time.Hour)},
		{Name: "warm-repo", PushedAt: now.Add(-10 * 24 * time.Hour)},
		{Name: "cold-repo", PushedAt: now.Add(-60 * 24 * time.Hour)},
		{Name: "dead-repo", PushedAt: now.Add(-400 * 24 * time.Hour)},
		{Name: "hidden-repo", PushedAt: now.Add(-1 * time.Hour)},
	}

	tiers := map[string]storage.RepoTier{
		"hot-repo":    storage.TierHot,
		"warm-repo":   storage.TierWarm,
		"cold-repo":   storage.TierCold,
		"dead-repo":   storage.TierDead,
		"hidden-repo": storage.TierHidden,
	}

	hiddenRepos := map[string]bool{
		"hidden-repo": true,
	}

	m := Model{
		ActiveTab:       TabRepos,
		Repos:           repos,
		RepoTiers:       tiers,
		HiddenRepos:     hiddenRepos,
		GitStatuses:     make(map[string]git.RepoGitStatus),
		Width:           140,
		Height:          30,
		SplashDismissed: true,
		CollapsedTiers: map[storage.RepoTier]bool{
			storage.TierDead:   true,
			storage.TierHidden: true,
		},
	}

	// 1. Initial State: Dead and Hidden must be collapsed by default
	if !m.isTierCollapsed(storage.TierDead) {
		t.Fatalf("expected Dead tier to be collapsed by default")
	}
	if !m.isTierCollapsed(storage.TierHidden) {
		t.Fatalf("expected Hidden tier to be collapsed by default")
	}
	if m.isTierCollapsed(storage.TierHot) {
		t.Fatalf("expected Hot tier to be expanded by default")
	}

	items := m.getRepoTableItems()
	// Hot Hdr (0), hot-repo (1), Warm Hdr (2), warm-repo (3), Cold Hdr (4), cold-repo (5), Dead Hdr (6), Hidden Hdr (7)
	if len(items) != 8 {
		t.Fatalf("expected 8 items with Dead and Hidden collapsed, got %d", len(items))
	}
	if items[6].Tier != storage.TierDead || items[6].Expanded {
		t.Fatalf("expected item 6 to be collapsed Dead tier header")
	}
	if items[7].Tier != storage.TierHidden || items[7].Expanded {
		t.Fatalf("expected item 7 to be collapsed Hidden tier header")
	}

	// 2. View Rendering: should display hint to expand on collapsed tiers, hint to collapse on expanded tiers
	view := m.renderReposView()
	if !strings.Contains(view, "[Space to expand]") {
		t.Fatalf("expected view to contain '[Space to expand]', got:\n%s", view)
	}
	if !strings.Contains(view, "[Space to collapse]") {
		t.Fatalf("expected view to contain '[Space to collapse]', got:\n%s", view)
	}

	// 3. Footer shows contextual controls when cursor is on Tier Header
	m.RepoCursor = 0 // Hot Tier Header
	footer := m.renderFooter()
	if !strings.Contains(footer, "Toggle Tier") || !strings.Contains(footer, "Collapse All") {
		t.Fatalf("expected tier header controls in footer when cursor on header, got:\n%s", footer)
	}

	// 4. Press Space on Hot Header (index 0) -> collapses Hot tier!
	updated, _ := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m = updated.(Model)
	if !m.isTierCollapsed(storage.TierHot) {
		t.Fatalf("expected Hot tier to be collapsed after Space")
	}

	// 5. Press Left on a child repo row -> jumps cursor to parent tier header
	m.RepoCursor = 2 // warm-repo
	if m.getRepoTableItems()[m.RepoCursor].Kind != ItemRepo {
		t.Fatalf("expected cursor at ItemRepo, got %+v", m.getRepoTableItems()[m.RepoCursor])
	}
	updated, _ = m.handleReposKeys(tea.KeyMsg{Type: tea.KeyLeft})
	m = updated.(Model)
	if m.RepoCursor != 1 || m.getRepoTableItems()[m.RepoCursor].Tier != storage.TierWarm {
		t.Fatalf("expected cursor to jump to Warm header at index 1, got index %d", m.RepoCursor)
	}

	// 6. Press 'C' (Shift-C) -> collapses all tiers!
	updated, _ = m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'C'}})
	m = updated.(Model)
	for _, tName := range []storage.RepoTier{storage.TierHot, storage.TierWarm, storage.TierCold, storage.TierDead, storage.TierHidden} {
		if !m.isTierCollapsed(tName) {
			t.Fatalf("expected tier %s to be collapsed after Shift-C", tName)
		}
	}
	allCollapsedItems := m.getRepoTableItems()
	if len(allCollapsedItems) != 5 {
		t.Fatalf("expected 5 tier headers when all collapsed, got %d", len(allCollapsedItems))
	}

	// 7. Press 'E' (Shift-E) -> expands all tiers!
	updated, _ = m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'E'}})
	m = updated.(Model)
	for _, tName := range []storage.RepoTier{storage.TierHot, storage.TierWarm, storage.TierCold, storage.TierDead, storage.TierHidden} {
		if m.isTierCollapsed(tName) {
			t.Fatalf("expected tier %s to be expanded after Shift-E", tName)
		}
	}
	allExpandedItems := m.getRepoTableItems()
	if len(allExpandedItems) != 10 { // 5 headers + 5 repos
		t.Fatalf("expected 10 items when all expanded, got %d", len(allExpandedItems))
	}
}

func TestMultiOrgRepoDeduplication(t *testing.T) {
	// 1. Test InitialModel deduplication with SQLite DB containing duplicates
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("storage.Open failed: %v", err)
	}
	defer db.Close()

	now := time.Now()
	_ = db.SaveRepositories([]github.Repository{
		{
			ID:        101,
			Name:      "tessell",
			FullName:  "bronners-org/tessell",
			Owner:     "bronners-org",
			PushedAt:  now,
			UpdatedAt: now,
		},
	}, "bronners-org")

	cfg := &config.Config{
		ActiveSquad: "core",
		Squads: []config.SquadConfig{
			{
				Name:          "core",
				Organizations: []string{"simonbronner", "bronners-org"},
			},
		},
	}

	m := InitialModel(cfg, nil, nil, "bronners-org", db)
	if len(m.Repos) != 1 {
		t.Fatalf("expected 1 repo loaded from DB without duplicates, got %d", len(m.Repos))
	}
	if m.Repos[0].FullName != "bronners-org/tessell" {
		t.Fatalf("expected bronners-org/tessell, got %s", m.Repos[0].FullName)
	}

	// 2. Test DataLoadedMsg updates m.Repos without duplicates
	dupRepos := []github.Repository{
		{
			ID:       101,
			Name:     "tessell",
			FullName: "bronners-org/tessell",
			Owner:    "bronners-org",
		},
		{
			ID:       102,
			Name:     "clj-stamp-duty",
			FullName: "bronners-org/clj-stamp-duty",
			Owner:    "bronners-org",
		},
	}

	updated, _ := m.Update(DataLoadedMsg{
		Repos: dupRepos,
	})
	m = updated.(Model)

	if len(m.Repos) != 2 {
		t.Fatalf("expected 2 unique repos, got %d", len(m.Repos))
	}
}

func TestReposLeaderKeys_FilterOpenYankClaim(t *testing.T) {
	m := createTestModel()
	m.Config.ActiveSquad = "backend-squad"
	m.Config.Squads = []config.SquadConfig{
		{
			Name:        "backend-squad",
			DisplayName: "Backend Squad",
			Members:     []string{"alice"},
			Repos:       []string{},
		},
	}

	// Focus first repo (auth-service)
	for idx, it := range m.getRepoTableItems() {
		if it.Kind == ItemRepo && it.Repo != nil && it.Repo.Name == "auth-service" {
			m.RepoCursor = idx
			break
		}
	}

	// 1. Test [f]ilter leader -> [n]ame modal
	updated, _ := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = updated.(Model)
	if m.RepoLeaderMode != RepoLeaderFilter {
		t.Fatalf("expected RepoLeaderFilter, got %v", m.RepoLeaderMode)
	}
	updated, _ = m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(Model)
	if m.ActiveModal != ModalFilterRepoName {
		t.Fatalf("expected ModalFilterRepoName, got %v", m.ActiveModal)
	}
	m.ActiveModal = ModalNone

	// 2. Test [f]ilter leader -> [l]abel modal
	updated, _ = m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = updated.(Model)
	updated, _ = m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updated.(Model)
	if m.ActiveModal != ModalFilterGroup {
		t.Fatalf("expected ModalFilterGroup, got %v", m.ActiveModal)
	}
	m.ActiveModal = ModalNone

	// 3. Test [f]ilter leader -> [s]quad toggle
	updated, _ = m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = updated.(Model)
	updated, cmdS := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(Model)
	if !m.SquadFilterActive {
		t.Fatalf("expected SquadFilterActive=true")
	}
	if cmdS == nil {
		t.Fatalf("expected toast message for squad filter toggle")
	}

	// Toggle back off
	updated, _ = m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = updated.(Model)
	updated, _ = m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(Model)
	if m.SquadFilterActive {
		t.Fatalf("expected SquadFilterActive=false")
	}

	// 4. Test [C]laim replaces [O]
	for idx, it := range m.getRepoTableItems() {
		if it.Kind == ItemRepo && it.Repo != nil && it.Repo.Name == "auth-service" {
			m.RepoCursor = idx
			break
		}
	}
	if m.Config.IsRepoOwnedBySquad("backend-squad", "auth-service") {
		t.Fatalf("expected auth-service not to be owned initially")
	}
	updated, cmdC := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'C'}})
	m = updated.(Model)
	if !m.Config.IsRepoOwnedBySquad("backend-squad", "auth-service") {
		t.Fatalf("expected auth-service to be owned after [C]laim")
	}
	if cmdC == nil {
		t.Fatalf("expected toast message cmd after claim")
	}

	// 5. Test [o]pen leader -> [p] (PRs in browser)
	updated, _ = m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	m = updated.(Model)
	if m.RepoLeaderMode != RepoLeaderOpen {
		t.Fatalf("expected RepoLeaderOpen, got %v", m.RepoLeaderMode)
	}
	updated, cmdOpenP := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(Model)
	if cmdOpenP == nil {
		t.Fatalf("expected toast cmd after open PRs")
	}
	toast := string(cmdOpenP().(StatusToastMsg))
	if !strings.Contains(toast, "Pull Requests in browser") {
		t.Errorf("expected toast to mention Pull Requests, got %q", toast)
	}

	// 6. Test [y]ank leader -> [p] (PRs URL yanked)
	updated, _ = m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(Model)
	if m.RepoLeaderMode != RepoLeaderYank {
		t.Fatalf("expected RepoLeaderYank, got %v", m.RepoLeaderMode)
	}
	updated, cmdYankP := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(Model)
	if cmdYankP == nil {
		t.Fatalf("expected toast cmd after yank PRs URL")
	}
	yankToast := string(cmdYankP().(StatusToastMsg))
	if !strings.Contains(yankToast, "PRs URL to clipboard") {
		t.Errorf("expected toast to mention PRs URL to clipboard, got %q", yankToast)
	}

	// 7. Test [y]ank leader -> [s] (session name yanked)
	updated, _ = m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(Model)
	updated, cmdYankS := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(Model)
	if cmdYankS == nil {
		t.Fatalf("expected toast cmd after yank session")
	}
	sessToast := string(cmdYankS().(StatusToastMsg))
	if !strings.Contains(sessToast, "Copied session name") {
		t.Errorf("expected toast to mention copied session name, got %q", sessToast)
	}
}

func TestReposView_SortLeader_Options(t *testing.T) {
	now := time.Now()
	repos := []github.Repository{
		{Name: "repo-low-vuln", FullName: "org/repo-low-vuln", PushedAt: now.Add(-10 * time.Hour)},
		{Name: "repo-high-vuln", FullName: "org/repo-high-vuln", PushedAt: now.Add(-5 * time.Hour)},
		{Name: "repo-many-prs", FullName: "org/repo-many-prs", PushedAt: now.Add(-1 * time.Hour)},
		{Name: "repo-newest", FullName: "org/repo-newest", PushedAt: now},
	}

	secSummaries := map[string]github.SecurityAlertSummary{
		"repo-low-vuln":  {Total: 2, High: 1},
		"repo-high-vuln": {Total: 20, Critical: 5, High: 10},
		"repo-many-prs":  {Total: 0},
		"repo-newest":    {Total: 0},
	}

	prs := []github.PullRequest{
		{Number: 1, Repo: "repo-many-prs", RepoFullName: "org/repo-many-prs", Title: "PR 1", ReviewDecision: "APPROVED"},
		{Number: 2, Repo: "repo-many-prs", RepoFullName: "org/repo-many-prs", Title: "PR 2", ReviewDecision: "REVIEW_REQUIRED"},
		{Number: 3, Repo: "repo-low-vuln", RepoFullName: "org/repo-low-vuln", Title: "PR 3", ReviewDecision: "APPROVED"},
	}

	m := Model{
		ActiveTab:         TabRepos,
		Repos:             repos,
		SecuritySummaries: secSummaries,
		PRs:               prs,
		ExpandedRepoPRs:   make(map[string]bool),
		Width:             120,
		Height:            30,
	}

	// 1. Press [S] -> verifies RepoLeaderSort is entered
	updated, _ := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	m = updated.(Model)
	if m.RepoLeaderMode != RepoLeaderSort {
		t.Fatalf("expected RepoLeaderSort, got %v", m.RepoLeaderMode)
	}

	// 2. Press [v] -> sort by vulnerabilities
	updated, cmdV := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	m = updated.(Model)
	if m.RepoSortMode != RepoSortVulns {
		t.Fatalf("expected RepoSortVulns, got %v", m.RepoSortMode)
	}
	if cmdV == nil {
		t.Fatalf("expected toast cmd for vulns sort")
	}
	sortedRepos := m.getDisplayRepos()
	if sortedRepos[0].Name != "repo-high-vuln" {
		t.Fatalf("expected repo-high-vuln first in vuln sort, got %s", sortedRepos[0].Name)
	}

	// 3. Press [S] then [p] -> sort by pull requests & auto-expand repos with open PRs
	updated, _ = m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	m = updated.(Model)
	updated, cmdP := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(Model)
	if m.RepoSortMode != RepoSortPRs {
		t.Fatalf("expected RepoSortPRs, got %v", m.RepoSortMode)
	}
	if cmdP == nil {
		t.Fatalf("expected toast cmd for PRs sort")
	}
	sortedRepos = m.getDisplayRepos()
	if sortedRepos[0].Name != "repo-many-prs" {
		t.Fatalf("expected repo-many-prs first in PRs sort (has 2 PRs), got %s", sortedRepos[0].Name)
	}
	// Verify auto-expansion of repos with PRs
	if !m.isRepoPRExpanded(&repos[2]) { // repo-many-prs
		t.Fatalf("expected repo-many-prs to be auto-expanded in PR sort")
	}

	// 4. Press [S] then [l] -> sort by last updated
	updated, _ = m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	m = updated.(Model)
	updated, cmdL := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updated.(Model)
	if m.RepoSortMode != RepoSortLastUpdated {
		t.Fatalf("expected RepoSortLastUpdated, got %v", m.RepoSortMode)
	}
	if cmdL == nil {
		t.Fatalf("expected toast cmd for last updated sort")
	}
	sortedRepos = m.getDisplayRepos()
	if sortedRepos[0].Name != "repo-newest" {
		t.Fatalf("expected repo-newest first in last updated sort, got %s", sortedRepos[0].Name)
	}

	// 5. Press [Esc] -> clears sorting to default
	updated, cmdEsc := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.RepoSortMode != RepoSortDefault {
		t.Fatalf("expected RepoSortDefault after esc, got %v", m.RepoSortMode)
	}
	if cmdEsc == nil {
		t.Fatalf("expected toast cmd after clearing sort with esc")
	}
}

func TestReposView_SessionTopLevelCommand(t *testing.T) {
	repos := []github.Repository{
		{Name: "my-cloned-repo", FullName: "org/my-cloned-repo"},
	}
	gitStatuses := map[string]git.RepoGitStatus{
		"my-cloned-repo": {IsCloned: true, LocalPath: "/tmp/my-cloned-repo"},
	}
	prs := []github.PullRequest{
		{Number: 10, Repo: "my-cloned-repo", RepoFullName: "org/my-cloned-repo", Title: "PR 10"},
	}

	m := Model{
		ActiveTab:       TabRepos,
		Repos:           repos,
		GitStatuses:     gitStatuses,
		PRs:             prs,
		ExpandedRepoPRs: map[string]bool{"my-cloned-repo": true, "org/my-cloned-repo": true},
		Width:           120,
		Height:          30,
	}

	// 1. On Tier Header: pressing 's' shows toast
	m.RepoCursor = 0 // Tier header
	items := m.getRepoTableItems()
	if len(items) == 0 || items[0].Kind != ItemTierHeader {
		t.Fatalf("expected item 0 to be ItemTierHeader")
	}
	_, cmdHeaderS := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if cmdHeaderS == nil {
		t.Fatalf("expected toast cmd for session on tier header")
	}
	toast := string(cmdHeaderS().(StatusToastMsg))
	if !strings.Contains(toast, "Select a repository to open session") {
		t.Errorf("expected toast mentioning selecting a repo, got %q", toast)
	}

	// 2. On Repo row: pressing 's' switches/creates session
	for idx, it := range items {
		if it.Kind == ItemRepo {
			m.RepoCursor = idx
			break
		}
	}
	_, cmdRepoS := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if cmdRepoS == nil {
		t.Fatalf("expected cmd for session on repo row")
	}

	// 3. On PR row: pressing 's' launches session for parent repo
	for idx, it := range items {
		if it.Kind == ItemRepoPR {
			m.RepoCursor = idx
			break
		}
	}
	_, cmdPRS := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if cmdPRS == nil {
		t.Fatalf("expected cmd for session on PR row")
	}
}

func TestReposView_AbsorbedPRCapabilities(t *testing.T) {
	repos := []github.Repository{
		{Name: "grubby", FullName: "bronners-org/grubby"},
	}
	gitStatuses := map[string]git.RepoGitStatus{
		"grubby": {IsCloned: true, LocalPath: "/tmp/grubby"},
	}
	prs := []github.PullRequest{
		{
			Number:         12,
			Repo:           "grubby",
			RepoFullName:   "bronners-org/grubby",
			Title:          "Approved PR",
			Author:         "alice",
			ReviewDecision: "APPROVED",
			URL:            "https://github.com/bronners-org/grubby/pull/12",
		},
		{
			Number:         13,
			Repo:           "grubby",
			RepoFullName:   "bronners-org/grubby",
			Title:          "Pending Review PR",
			Author:         "bob",
			ReviewDecision: "REVIEW_REQUIRED",
			URL:            "https://github.com/bronners-org/grubby/pull/13",
		},
	}

	m := Model{
		ActiveTab:       TabRepos,
		Repos:           repos,
		GitStatuses:     gitStatuses,
		PRs:             prs,
		ExpandedRepoPRs: map[string]bool{"grubby": true, "bronners-org/grubby": true},
		Width:           120,
		Height:          30,
	}

	items := m.getRepoTableItems()
	pr12Idx := -1
	pr13Idx := -1
	for idx, it := range items {
		if it.Kind == ItemRepoPR && it.PR != nil {
			if it.PR.Number == 12 {
				pr12Idx = idx
			} else if it.PR.Number == 13 {
				pr13Idx = idx
			}
		}
	}
	if pr12Idx == -1 || pr13Idx == -1 {
		t.Fatalf("expected PR items in table, got items: %+v", items)
	}

	// 1. Merge approved PR #12 with 'm'
	m.RepoCursor = pr12Idx
	_, cmdMerge := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	if cmdMerge == nil {
		t.Fatalf("expected merge cmd for approved PR #12")
	}

	// 2. Reject merge on non-approved PR #13 with 'm'
	m.RepoCursor = pr13Idx
	_, cmdReject := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	if cmdReject == nil {
		t.Fatalf("expected toast cmd for non-approved PR #13")
	}
	toastMsg := string(cmdReject().(StatusToastMsg))
	if !strings.Contains(toastMsg, "requires APPROVED") {
		t.Errorf("expected toast mentioning requires APPROVED, got %q", toastMsg)
	}

	// 3. Checkout branch with 'k' on PR #12
	m.RepoCursor = pr12Idx
	_, cmdCheckoutK := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if cmdCheckoutK == nil {
		t.Fatalf("expected checkout cmd with 'k'")
	}

	// 4. Checkout branch with 'c' on PR #12
	_, cmdCheckoutC := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if cmdCheckoutC == nil {
		t.Fatalf("expected checkout cmd with 'c'")
	}

	// 5. Yank PR URL with 'y' on PR #12
	_, cmdYank := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if cmdYank == nil {
		t.Fatalf("expected yank cmd with 'y'")
	}
	yankToast := string(cmdYank().(StatusToastMsg))
	if !strings.Contains(yankToast, "https://github.com/bronners-org/grubby/pull/12") {
		t.Errorf("expected yank toast containing PR URL, got %q", yankToast)
	}

	// 6. Request Copilot review with 'p' on PR #12
	_, cmdCopilot := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	if cmdCopilot == nil {
		t.Fatalf("expected copilot cmd with 'p'")
	}

	// 7. Create review task with 't' on PR #12
	updated, _ := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m = updated.(Model)
	if m.ActiveModal != ModalCreatePRTask {
		t.Fatalf("expected ModalCreatePRTask after 't', got %v", m.ActiveModal)
	}
	if m.PRTaskTarget.Number != 12 {
		t.Fatalf("expected PRTaskTarget #12, got #%d", m.PRTaskTarget.Number)
	}
}

func TestTopLevelTabs_WithoutPRTab(t *testing.T) {
	m := Model{
		ActiveTab:       TabRepos,
		SplashDismissed: true,
		Width:           140,
		Height:          30,
	}

	// 1. Render Top Bar and verify exactly 5 tabs, no PR tab
	topBar := m.renderTopBar()
	if strings.Contains(topBar, "Pull Requests") || strings.Contains(topBar, "2: PRs") {
		t.Errorf("expected top bar NOT to contain PR tab, got:\n%s", topBar)
	}
	if !strings.Contains(topBar, "1: Repositories") || !strings.Contains(topBar, "2: Tasks") || !strings.Contains(topBar, "3: Squad Activity") {
		t.Errorf("expected top bar to have Repos, Tasks, Squad Activity, got:\n%s", topBar)
	}

	// 2. Test numeric tab navigation: '1'=Repos, '2'=Tasks, '3'=Squad, '4'=Search, '5'=Status
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = updated.(Model)
	if m.ActiveTab != TabTasks {
		t.Errorf("expected ActiveTab=TabTasks on '2', got %v", m.ActiveTab)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	m = updated.(Model)
	if m.ActiveTab != TabSquad {
		t.Errorf("expected ActiveTab=TabSquad on '3', got %v", m.ActiveTab)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	m = updated.(Model)
	if m.ActiveTab != TabSearch {
		t.Errorf("expected ActiveTab=TabSearch on '4', got %v", m.ActiveTab)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	m = updated.(Model)
	if m.ActiveTab != TabStatus {
		t.Errorf("expected ActiveTab=TabStatus on '5', got %v", m.ActiveTab)
	}

	// 3. Test Tab cycling (modulo 5)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.ActiveTab != TabRepos {
		t.Errorf("expected TabRepos after cycling from 5, got %v", m.ActiveTab)
	}
}

func TestRepositoryRefreshDirectQuery(t *testing.T) {
	m := createTestModel()
	m.ActiveTab = TabRepos

	// 1. Footer contains [r] Refresh
	m.RepoCursor = m.firstSelectableRepoIndex()
	footer := m.renderFooter()
	if !strings.Contains(footer, "[r] Refresh") {
		t.Fatalf("expected footer to contain '[r] Refresh', got:\n%s", footer)
	}

	// 2. Press 'r' on repo row
	selectedRepo := m.getSelectedRepo()
	if selectedRepo == nil {
		t.Fatalf("expected selected repo at index %d", m.RepoCursor)
	}
	updated, cmd := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated.(Model)
	if cmd == nil {
		t.Fatalf("expected non-nil cmd on pressing 'r'")
	}
	expectedStatus := fmt.Sprintf("Refreshing %s directly from GitHub...", selectedRepo.Name)
	if m.StatusMsg != expectedStatus {
		t.Errorf("expected status %q, got %q", expectedStatus, m.StatusMsg)
	}

	// 3. Press 'R' (capital) on repo row
	updated, cmd = m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	m = updated.(Model)
	if cmd == nil || m.StatusMsg != expectedStatus {
		t.Errorf("expected 'R' to also trigger refresh, got status %q", m.StatusMsg)
	}

	// 4. Press 'r' on tier header
	m.RepoCursor = 0 // Tier header
	updated, cmd = m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated.(Model)
	if cmd == nil {
		t.Fatalf("expected toast cmd on pressing 'r' on tier header")
	}
	msg := cmd()
	if toast, ok := msg.(StatusToastMsg); !ok || string(toast) != "Select a repository to refresh" {
		t.Errorf("expected toast 'Select a repository to refresh', got %v", msg)
	}

	// 5. Press 'r' on PR row refreshes parent repo
	pr := github.PullRequest{
		Number:       10,
		Repo:         selectedRepo.Name,
		RepoFullName: selectedRepo.FullName,
		Title:        "Test PR for refresh",
		HeadRefName:  "feature/test",
	}
	m.PRs = []github.PullRequest{pr}
	m.ExpandedRepoPRs = map[string]bool{strings.ToLower(selectedRepo.Name): true}
	items := m.getRepoTableItems()
	prIdx := -1
	for idx, it := range items {
		if it.Kind == ItemRepoPR {
			prIdx = idx
			break
		}
	}
	if prIdx == -1 {
		t.Fatalf("expected expanded PR row in repo table")
	}
	m.RepoCursor = prIdx
	// Verify footer shows [r] Refresh on PR row too
	prFooter := m.renderFooter()
	if !strings.Contains(prFooter, "[r] Refresh") {
		t.Errorf("expected PR footer to contain '[r] Refresh', got:\n%s", prFooter)
	}

	updated, cmd = m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated.(Model)
	if cmd == nil || m.StatusMsg != expectedStatus {
		t.Errorf("expected 'r' on PR row to refresh parent repo %q, got status %q", selectedRepo.Name, m.StatusMsg)
	}

	// 6. Test SingleRepoLoadedMsg processing
	now := time.Now()
	newPR := github.PullRequest{
		Number:       11,
		Repo:         selectedRepo.Name,
		RepoFullName: selectedRepo.FullName,
		Title:        "Newly loaded PR via direct query",
		ReviewDecision: "APPROVED",
	}
	loadedRepo := *selectedRepo
	loadedRepo.PushedAt = now
	loadedMsg := SingleRepoLoadedMsg{
		Repo: &loadedRepo,
		PRs:  []github.PullRequest{newPR},
		Events: []github.SquadEvent{
			{Actor: "alice", Action: "Committed", Repo: selectedRepo.Name, Details: "test commit", Timestamp: now},
		},
		Security: &github.SecurityAlertSummary{Repo: selectedRepo.Name, Total: 0},
	}

	updatedModel, _ := m.Update(loadedMsg)
	m = updatedModel.(Model)
	if !strings.Contains(m.StatusMsg, fmt.Sprintf("✓ Refreshed %s", selectedRepo.Name)) {
		t.Errorf("expected success status message, got %q", m.StatusMsg)
	}

	// Verify PRs were updated with the direct query results
	foundRefreshedPR := false
	for _, p := range m.PRs {
		if p.Number == 11 && strings.EqualFold(p.Repo, selectedRepo.Name) {
			foundRefreshedPR = true
			break
		}
	}
	if !foundRefreshedPR {
		t.Errorf("expected PR #11 to be in m.PRs after direct query refresh")
	}

	// 7. Test 'r' on TabRepos via top-level Update delegates to handleReposKeys instead of full loadDataCmd
	m.RepoCursor = m.firstSelectableRepoIndex()
	m.Loading = false
	updatedTop, topCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	mTop := updatedTop.(Model)
	if mTop.StatusMsg != expectedStatus {
		t.Errorf("expected top-level 'r' on TabRepos to set single repo refresh status, got %q", mTop.StatusMsg)
	}
	if topCmd == nil {
		t.Errorf("expected non-nil cmd from top-level 'r'")
	}
}

func TestHiddenRepoNotificationSuppression(t *testing.T) {
	m := createTestModel()
	m.HiddenRepos = map[string]bool{
		"auth-service": true,
	}

	// 1. DataLoadedMsg with notifications: auth-service (hidden) vs api-gateway (visible)
	notifHidden := github.Notification{
		ID:         "notif-1",
		Repository: "corp/auth-service",
		Title:      "PR in hidden auth-service",
		Type:       "PullRequest",
		Unread:     true,
	}
	notifVisible := github.Notification{
		ID:         "notif-2",
		Repository: "corp/api-gateway",
		Title:      "PR in visible api-gateway",
		Type:       "PullRequest",
		Unread:     true,
	}

	dataMsg := DataLoadedMsg{
		Notifications: []github.Notification{notifHidden, notifVisible},
	}

	updated, _ := m.Update(dataMsg)
	m = updated.(Model)

	// Verify hidden notification is NOT in m.Notifications
	if len(m.Notifications) != 1 {
		t.Fatalf("expected 1 active notification, got %d", len(m.Notifications))
	}
	if m.Notifications[0].ID != "notif-2" {
		t.Errorf("expected visible notification 'notif-2', got %s", m.Notifications[0].ID)
	}
	// Verify hidden notification was marked in SeenNotificationIDs so it will never alert later
	if !m.SeenNotificationIDs["notif-1"] {
		t.Errorf("expected SeenNotificationIDs['notif-1'] to be true")
	}

	// 2. Toggling 'h' on a repository removes its notifications from m.Notifications
	m.Notifications = []github.Notification{
		{ID: "n-3", Repository: "api-gateway", Title: "Notice for api-gateway"},
	}
	m.CollapsedTiers = map[storage.RepoTier]bool{
		storage.TierDead:   false,
		storage.TierHidden: false,
	}
	// Cursor on api-gateway
	items := m.getRepoTableItems()
	for idx, it := range items {
		if it.Kind == ItemRepo && it.Repo != nil && it.Repo.Name == "api-gateway" {
			m.RepoCursor = idx
			break
		}
	}
	// Press 'h' to hide api-gateway
	updatedH, _ := m.handleReposKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	mH := updatedH.(Model)
	if !mH.isRepoHidden("api-gateway") {
		t.Errorf("expected api-gateway to be hidden after 'h'")
	}
	if len(mH.Notifications) != 0 {
		t.Errorf("expected notifications for api-gateway to be purged from m.Notifications, got %d", len(mH.Notifications))
	}
}

func TestGetGitStatusResolution(t *testing.T) {
	m := createTestModel()
	m.GitStatuses = make(map[string]git.RepoGitStatus)

	mainSt := git.RepoGitStatus{
		RepoName:    "auth-service",
		LocalPath:   "/workspaces/auth-service",
		IsCloned:    true,
		Branch:      "main",
		State:       git.StateSynced,
		HasUpstream: true,
	}

	m.GitStatuses["auth-service"] = mainSt
	m.GitStatuses["acme/auth-service"] = mainSt
	m.GitStatuses["auth-service.git"] = mainSt

	// 1. Exact match
	st := m.getGitStatus("auth-service")
	if !st.IsCloned || st.Branch != "main" {
		t.Errorf("expected cloned auth-service, got %+v", st)
	}

	// 2. Full name match
	st = m.getGitStatus("acme/auth-service")
	if !st.IsCloned || st.Branch != "main" {
		t.Errorf("expected cloned for full name acme/auth-service, got %+v", st)
	}

	// 3. Short name extracted from full name
	st = m.getGitStatus("other-org/auth-service")
	if !st.IsCloned || st.Branch != "main" {
		t.Errorf("expected cloned for short name resolution from other-org/auth-service, got %+v", st)
	}

	// 4. Normalized name (.git suffix removed)
	st = m.getGitStatus("auth-service.git")
	if !st.IsCloned || st.Branch != "main" {
		t.Errorf("expected cloned for normalized auth-service.git, got %+v", st)
	}

	// 5. Unknown repo returns StateNotCloned
	unknown := m.getGitStatus("completely-unknown-repo")
	if unknown.IsCloned || unknown.State != git.StateNotCloned {
		t.Errorf("expected not cloned for unknown repo, got %+v", unknown)
	}
}

func TestGitStatusUpdatedMsg_PreservesOtherRepos(t *testing.T) {
	m := createTestModel()
	m.GitStatuses = make(map[string]git.RepoGitStatus)

	// Initially we have auth-service and web-client in GitStatuses
	m.GitStatuses["auth-service"] = git.RepoGitStatus{
		RepoName:    "auth-service",
		LocalPath:   "/workspaces/auth-service",
		IsCloned:    true,
		Branch:      "main",
		State:       git.StateSynced,
		HasUpstream: true,
	}
	m.GitStatuses["web-client"] = git.RepoGitStatus{
		RepoName:    "web-client",
		LocalPath:   "/workspaces/web-client",
		IsCloned:    true,
		Branch:      "feat-x",
		IsDirty:     true,
		Ahead:       1,
		State:       git.StateDirty,
		HasUpstream: true,
	}

	// Now simulate a refresh on just "api-gateway"
	singleRefreshMsg := GitStatusUpdatedMsg{
		Statuses: map[string]git.RepoGitStatus{
			"api-gateway": {
				RepoName:    "api-gateway",
				LocalPath:   "/workspaces/api-gateway",
				IsCloned:    true,
				Branch:      "develop",
				State:       git.StateSynced,
				HasUpstream: true,
			},
		},
	}

	updated, _ := m.Update(singleRefreshMsg)
	m = updated.(Model)

	// Verify api-gateway was added
	if st := m.getGitStatus("api-gateway"); !st.IsCloned || st.Branch != "develop" {
		t.Errorf("expected api-gateway to be updated, got %+v", st)
	}

	// CRITICAL CHECK: auth-service and web-client MUST NOT have been wiped out!
	if st := m.getGitStatus("auth-service"); !st.IsCloned || st.Branch != "main" {
		t.Errorf("CRITICAL BUG: auth-service was wiped out when api-gateway was refreshed! Got %+v", st)
	}
	if st := m.getGitStatus("web-client"); !st.IsCloned || !st.IsDirty || st.Ahead != 1 {
		t.Errorf("CRITICAL BUG: web-client was wiped out when api-gateway was refreshed! Got %+v", st)
	}
}

func TestGitBadgeRenderingInRepoView(t *testing.T) {
	m := createTestModel()
	m.Width = 160
	m.Height = 40
	m.GitStatuses = make(map[string]git.RepoGitStatus)

	// auth-service is synced on main
	m.GitStatuses["auth-service"] = git.RepoGitStatus{
		RepoName:    "auth-service",
		LocalPath:   "/workspaces/auth-service",
		IsCloned:    true,
		Branch:      "main",
		State:       git.StateSynced,
		HasUpstream: true,
	}

	// web-client is dirty on feat-ui with ahead 1
	m.GitStatuses["web-client"] = git.RepoGitStatus{
		RepoName:    "web-client",
		LocalPath:   "/workspaces/web-client",
		IsCloned:    true,
		Branch:      "feat-ui",
		IsDirty:     true,
		Ahead:       1,
		State:       git.StateDirty,
		HasUpstream: true,
	}

	// docs is not cloned
	m.GitStatuses["docs"] = git.RepoGitStatus{
		RepoName: "docs",
		IsCloned: false,
		State:    git.StateNotCloned,
	}

	// Render the repo list
	view := m.View()

	// Verify synced badge text "✓ main" is present
	if !strings.Contains(view, "✓ main") {
		t.Errorf("expected view to contain '✓ main' for synced auth-service, got:\n%s", view)
	}

	// Verify dirty badge text "* feat-ui ↑1" is present
	if !strings.Contains(view, "* feat-ui ↑1") {
		t.Errorf("expected view to contain '* feat-ui ↑1' for dirty web-client, got:\n%s", view)
	}

	// Verify not cloned badge text "[Not Cloned]" is present
	if !strings.Contains(view, "[Not Cloned]") {
		t.Errorf("expected view to contain '[Not Cloned]' for un-cloned docs, got:\n%s", view)
	}
}





