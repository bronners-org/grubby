package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigLoadAndSave(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.Version != 1 {
		t.Errorf("expected version 1, got %d", cfg.Version)
	}

	// Test Group operations
	added, err := cfg.ToggleRepoInGroup("api-gateway", "backend")
	if err != nil || !added {
		t.Fatalf("expected repo added to group, got added=%v, err=%v", added, err)
	}

	groups := cfg.GetGroupsForRepo("api-gateway")
	if len(groups) != 1 || groups[0] != "backend" {
		t.Errorf("expected group backend, got %v", groups)
	}

	// Add to second group
	added2, err := cfg.ToggleRepoInGroup("api-gateway", "core")
	if err != nil || !added2 {
		t.Fatalf("expected repo added to second group")
	}

	groups = cfg.GetGroupsForRepo("api-gateway")
	if len(groups) != 2 {
		t.Errorf("expected 2 groups, got %d", len(groups))
	}

	if !cfg.IsRepoInGroup("backend", "api-gateway") {
		t.Errorf("expected api-gateway to be in backend group")
	}
	if !cfg.IsRepoInGroup("core", "api-gateway") {
		t.Errorf("expected api-gateway to be in core group")
	}
	if cfg.IsRepoInGroup("frontend", "api-gateway") {
		t.Errorf("expected api-gateway NOT to be in frontend group")
	}

	// Test Squad operations
	err = cfg.AddMemberToSquad("platform", "alice")
	if err != nil {
		t.Fatalf("failed to add member: %v", err)
	}

	err = cfg.SetActiveSquad("platform")
	if err != nil {
		t.Fatalf("failed to set active squad: %v", err)
	}

	squad := cfg.GetActiveSquad()
	if squad == nil || squad.Name != "platform" || len(squad.Members) != 1 || squad.Members[0] != "alice" {
		t.Fatalf("unexpected active squad: %+v", squad)
	}

	// Reload from disk to verify persistence
	cfgReloaded, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("failed to reload config: %v", err)
	}

	if cfgReloaded.ActiveSquad != "platform" {
		t.Errorf("expected active squad platform, got %s", cfgReloaded.ActiveSquad)
	}

	reloadedGroups := cfgReloaded.GetGroupsForRepo("api-gateway")
	if len(reloadedGroups) != 2 {
		t.Errorf("expected 2 groups on reload, got %d", len(reloadedGroups))
	}

	// Test CollapseHome
	home, _ := os.UserHomeDir()
	collapsed := CollapseHome(filepath.Join(home, "workspaces", "grubby"))
	if collapsed != "~/workspaces/grubby" {
		t.Errorf("expected ~/workspaces/grubby, got %s", collapsed)
	}

	// Test RenameSquad, SetSquadMembers, and DeleteSquad
	err = cfg.SetSquadMembers("platform", []string{"alice", "bob", "charlie"})
	if err != nil {
		t.Fatalf("failed to set squad members: %v", err)
	}
	if len(cfg.GetActiveSquad().Members) != 3 {
		t.Fatalf("expected 3 members, got %d", len(cfg.GetActiveSquad().Members))
	}

	err = cfg.RenameSquad("platform", "infrastructure")
	if err != nil {
		t.Fatalf("failed to rename squad: %v", err)
	}
	if cfg.ActiveSquad != "infrastructure" {
		t.Errorf("expected active squad to be renamed to infrastructure, got %s", cfg.ActiveSquad)
	}

	err = cfg.DeleteSquad("infrastructure")
	if err != nil {
		t.Fatalf("failed to delete squad: %v", err)
	}
	if cfg.GetActiveSquad() != nil {
		t.Errorf("expected nil active squad after deleting only squad, got %+v", cfg.GetActiveSquad())
	}

	// Test Hidden Repos
	if cfg.IsRepoHidden("core-tasks") {
		t.Errorf("expected core-tasks not hidden initially")
	}
	nowHidden, err := cfg.ToggleRepoHidden("core-tasks")
	if err != nil || !nowHidden {
		t.Fatalf("expected ToggleRepoHidden to return true, got %v, err: %v", nowHidden, err)
	}
	if !cfg.IsRepoHidden("core-tasks") {
		t.Errorf("expected core-tasks to be hidden")
	}
	// Case insensitivity
	if !cfg.IsRepoHidden("CORE-TASKS") {
		t.Errorf("expected CORE-TASKS to be hidden case-insensitively")
	}
	nowHidden2, err := cfg.ToggleRepoHidden("core-tasks")
	if err != nil || nowHidden2 {
		t.Fatalf("expected ToggleRepoHidden to unhide, got %v, err: %v", nowHidden2, err)
	}
	if cfg.IsRepoHidden("core-tasks") {
		t.Errorf("expected core-tasks to be unhidden")
	}

	// Full name and short name matching
	_ = cfg.SetRepoHidden("corp/payment-service", true)
	if !cfg.IsRepoHidden("payment-service") || !cfg.IsRepoHidden("corp/payment-service") {
		t.Errorf("expected payment-service to be hidden for both short and full names")
	}
	_ = cfg.SetRepoHidden("auth-service", true)
	if !cfg.IsRepoHidden("corp/auth-service") || !cfg.IsRepoHidden("auth-service") {
		t.Errorf("expected auth-service to be hidden for both short and full names")
	}

	// Test Central Tasks Repo Resolution
	// 1. Unset: defaultOrg/grubby-tasks
	o, r := cfg.GetCentralTasksRepo("acme-corp")
	if o != "acme-corp" || r != "grubby-tasks" {
		t.Errorf("expected acme-corp/grubby-tasks, got %s/%s", o, r)
	}
	if fn := cfg.GetCentralTasksRepoFullName("acme-corp"); fn != "acme-corp/grubby-tasks" {
		t.Errorf("expected acme-corp/grubby-tasks, got %s", fn)
	}

	// 2. Explicit repo name only
	_ = cfg.SetCentralTasksRepo("tasks")
	o, r = cfg.GetCentralTasksRepo("acme-corp")
	if o != "acme-corp" || r != "tasks" {
		t.Errorf("expected acme-corp/tasks, got %s/%s", o, r)
	}

	// 3. Explicit owner/repo
	_ = cfg.SetCentralTasksRepo("custom-org/central-workspace")
	o, r = cfg.GetCentralTasksRepo("acme-corp")
	if o != "custom-org" || r != "central-workspace" {
		t.Errorf("expected custom-org/central-workspace, got %s/%s", o, r)
	}
	if fn := cfg.GetCentralTasksRepoFullName("acme-corp"); fn != "custom-org/central-workspace" {
		t.Errorf("expected custom-org/central-workspace, got %s", fn)
	}

	// Test Squad Repo Ownership
	_ = cfg.CreateSquad("security", "SecOps", []string{"eve"})
	if cfg.IsRepoOwnedBySquad("security", "auth-service") {
		t.Errorf("expected auth-service not owned initially")
	}

	// Claim ownership
	owned, err := cfg.ToggleRepoOwnership("security", "auth-service")
	if err != nil || !owned {
		t.Fatalf("expected ToggleRepoOwnership to return true, got %v, err: %v", owned, err)
	}
	if !cfg.IsRepoOwnedBySquad("security", "auth-service") {
		t.Errorf("expected auth-service to be owned by security squad")
	}

	// Another squad can also own the same repo (multiple squad owners)
	_ = cfg.CreateSquad("core", "Core Platform", []string{"simon"})
	owned2, _ := cfg.ToggleRepoOwnership("core", "auth-service")
	if !owned2 || !cfg.IsRepoOwnedBySquad("core", "auth-service") {
		t.Errorf("expected core squad to also own auth-service")
	}

	squadsForRepo := cfg.GetSquadsForRepo("auth-service")
	if len(squadsForRepo) != 2 {
		t.Fatalf("expected 2 squads owning auth-service, got %v", squadsForRepo)
	}

	reposForSecurity := cfg.GetReposForSquad("security")
	if len(reposForSecurity) != 1 || reposForSecurity[0] != "auth-service" {
		t.Errorf("expected [auth-service] for security squad, got %v", reposForSecurity)
	}

	// Unassign ownership
	unowned, _ := cfg.ToggleRepoOwnership("security", "auth-service")
	if unowned || cfg.IsRepoOwnedBySquad("security", "auth-service") {
		t.Errorf("expected auth-service to be unassigned from security")
	}
}

func TestConfigExists(t *testing.T) {
	tmpDir := t.TempDir()
	nonExistent := filepath.Join(tmpDir, "missing.yaml")
	if Exists(nonExistent) {
		t.Fatalf("expected Exists to return false for missing file")
	}

	existing := filepath.Join(tmpDir, "exists.yaml")
	if err := os.WriteFile(existing, []byte("version: 1\n"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
	if !Exists(existing) {
		t.Fatalf("expected Exists to return true for existing file")
	}
}

func TestSquadOrganizations(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	cfg.HomeOrg = "home-org"
	cfg.DefaultOrg = "default-org"

	err = cfg.CreateSquad("devops", "DevOps Squad", []string{"charlie"})
	if err != nil {
		t.Fatalf("failed to create squad: %v", err)
	}

	// 1. Initial orgs should be empty
	orgs := cfg.GetOrgsForSquad("devops")
	if len(orgs) != 0 {
		t.Fatalf("expected 0 orgs initially, got %v", orgs)
	}

	// 2. AddOrgToSquad
	err = cfg.AddOrgToSquad("devops", "org-alpha")
	if err != nil {
		t.Fatalf("failed to add org-alpha: %v", err)
	}
	err = cfg.AddOrgToSquad("devops", "org-beta")
	if err != nil {
		t.Fatalf("failed to add org-beta: %v", err)
	}
	// Adding duplicate org should be no-op / idempotent
	err = cfg.AddOrgToSquad("devops", "org-alpha")
	if err != nil {
		t.Fatalf("failed to add duplicate org: %v", err)
	}

	orgs = cfg.GetOrgsForSquad("devops")
	if len(orgs) != 2 || orgs[0] != "org-alpha" || orgs[1] != "org-beta" {
		t.Fatalf("expected [org-alpha, org-beta], got %v", orgs)
	}

	// 3. RemoveOrgFromSquad
	err = cfg.RemoveOrgFromSquad("devops", "org-alpha")
	if err != nil {
		t.Fatalf("failed to remove org-alpha: %v", err)
	}
	orgs = cfg.GetOrgsForSquad("devops")
	if len(orgs) != 1 || orgs[0] != "org-beta" {
		t.Fatalf("expected [org-beta], got %v", orgs)
	}

	// 4. SetSquadOrgs
	err = cfg.SetSquadOrgs("devops", []string{"org-1", "org-2", "org-3"})
	if err != nil {
		t.Fatalf("failed to set squad orgs: %v", err)
	}
	orgs = cfg.GetOrgsForSquad("devops")
	if len(orgs) != 3 {
		t.Fatalf("expected 3 orgs after SetSquadOrgs, got %v", orgs)
	}

	// 5. GetActiveSquadOrgs
	_ = cfg.SetActiveSquad("devops")
	activeOrgs := cfg.GetActiveSquadOrgs()
	if len(activeOrgs) != 3 || activeOrgs[0] != "org-1" {
		t.Fatalf("expected [org-1, org-2, org-3], got %v", activeOrgs)
	}

	// 6. GetTrackedOrgs combines DefaultOrg, HomeOrg, and active squad orgs
	tracked := cfg.GetTrackedOrgs()
	// Should contain default-org, home-org, org-1, org-2, org-3
	expectedTracked := map[string]bool{
		"default-org": true,
		"home-org":    true,
		"org-1":       true,
		"org-2":       true,
		"org-3":       true,
	}
	for _, tr := range tracked {
		if !expectedTracked[tr] {
			t.Errorf("unexpected tracked org: %s", tr)
		}
		delete(expectedTracked, tr)
	}
	if len(expectedTracked) != 0 {
		t.Errorf("missing tracked orgs: %v", expectedTracked)
	}

	// 7. Verify persistence across Load
	reloaded, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("failed to reload config: %v", err)
	}
	reloadedOrgs := reloaded.GetOrgsForSquad("devops")
	if len(reloadedOrgs) != 3 || reloadedOrgs[0] != "org-1" {
		t.Fatalf("expected 3 reloaded orgs, got %v", reloadedOrgs)
	}
}
