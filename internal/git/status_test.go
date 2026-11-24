package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestInspectRepoNotCloned(t *testing.T) {
	ctx := context.Background()
	st := InspectRepo(ctx, "non-existent", "/path/that/does/not/exist")
	if st.IsCloned {
		t.Errorf("expected IsCloned=false, got true")
	}
	if st.State != StateNotCloned {
		t.Errorf("expected StateNotCloned, got %s", st.State)
	}
	if st.Badge() != "[Not Cloned]" {
		t.Errorf("expected '[Not Cloned]' badge, got %s", st.Badge())
	}
}

func TestWorkspaceManager(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewWorkspaceManager(tmpDir)

	if mgr.WorkspaceDir() != tmpDir {
		t.Errorf("expected %s, got %s", tmpDir, mgr.WorkspaceDir())
	}

	repoPath := filepath.Join(tmpDir, "test-repo")
	_ = os.MkdirAll(repoPath, 0o755)

	statuses := mgr.ScanWorkspaces(context.Background(), []string{"test-repo"})
	st, ok := statuses["test-repo"]
	if !ok {
		t.Fatalf("expected test-repo in status map")
	}
	// No .git directory yet
	if st.IsCloned {
		t.Errorf("expected IsCloned=false for folder without .git")
	}
}

func TestBadgeAndShortBadge(t *testing.T) {
	tests := []struct {
		name       string
		status     RepoGitStatus
		maxWidth   int
		wantBadge  string
		wantShort  string
	}{
		{
			name: "not cloned",
			status: RepoGitStatus{
				IsCloned: false,
				State:    StateNotCloned,
			},
			maxWidth:  16,
			wantBadge: "[Not Cloned]",
			wantShort: "[Not Cloned]",
		},
		{
			name: "synced on main",
			status: RepoGitStatus{
				IsCloned:    true,
				Branch:      "main",
				State:       StateSynced,
				HasUpstream: true,
			},
			maxWidth:  16,
			wantBadge: "✓ main",
			wantShort: "✓ main",
		},
		{
			name: "clean dirty without ahead/behind",
			status: RepoGitStatus{
				IsCloned:    true,
				Branch:      "main",
				IsDirty:     true,
				State:       StateDirty,
				HasUpstream: true,
			},
			maxWidth:  16,
			wantBadge: "* main",
			wantShort: "* main",
		},
		{
			name: "dirty with ahead 1",
			status: RepoGitStatus{
				IsCloned:    true,
				Branch:      "main",
				IsDirty:     true,
				Ahead:       1,
				Behind:      0,
				State:       StateDirty,
				HasUpstream: true,
			},
			maxWidth:  16,
			wantBadge: "* main ↑1",
			wantShort: "* main ↑1",
		},
		{
			name: "dirty with ahead and behind",
			status: RepoGitStatus{
				IsCloned:    true,
				Branch:      "feat-x",
				IsDirty:     true,
				Ahead:       2,
				Behind:      1,
				State:       StateDirty,
				HasUpstream: true,
			},
			maxWidth:  16,
			wantBadge: "* feat-x ↑2 ↓1",
			wantShort: "* feat-x ↑2 ↓1",
		},
		{
			name: "clean ahead",
			status: RepoGitStatus{
				IsCloned:    true,
				Branch:      "feature",
				Ahead:       3,
				State:       StateAhead,
				HasUpstream: true,
			},
			maxWidth:  16,
			wantBadge: "↑3 feature",
			wantShort: "↑3 feature",
		},
		{
			name: "clean behind",
			status: RepoGitStatus{
				IsCloned:    true,
				Branch:      "feature",
				Behind:      2,
				State:       StateBehind,
				HasUpstream: true,
			},
			maxWidth:  16,
			wantBadge: "↓2 feature",
			wantShort: "↓2 feature",
		},
		{
			name: "clean diverged",
			status: RepoGitStatus{
				IsCloned:    true,
				Branch:      "feature",
				Ahead:       2,
				Behind:      3,
				State:       StateDiverged,
				HasUpstream: true,
			},
			maxWidth:  16,
			wantBadge: "⇡2 ⇣3 feature",
			wantShort: "⇡2 ⇣3 feature",
		},
		{
			name: "clean no upstream",
			status: RepoGitStatus{
				IsCloned:    true,
				Branch:      "spike",
				State:       StateNoUpstream,
				HasUpstream: false,
			},
			maxWidth:  16,
			wantBadge: "? spike",
			wantShort: "? spike",
		},
		{
			name: "long branch truncated in short badge",
			status: RepoGitStatus{
				IsCloned:    true,
				Branch:      "feature/a-very-long-branch-name-here",
				State:       StateSynced,
				HasUpstream: true,
			},
			maxWidth:  16,
			wantBadge: "✓ feature/a-very-long-branch-name-here",
			wantShort: "✓ feature/a-ver…",
		},
		{
			name: "dirty long branch with ahead truncated",
			status: RepoGitStatus{
				IsCloned:    true,
				Branch:      "feature/very-long-topic",
				IsDirty:     true,
				Ahead:       1,
				State:       StateDirty,
				HasUpstream: true,
			},
			maxWidth:  16,
			wantBadge: "* feature/very-long-topic ↑1",
			wantShort: "* feature/ve… ↑1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotBadge := tt.status.Badge()
			if gotBadge != tt.wantBadge {
				t.Errorf("Badge() = %q, want %q", gotBadge, tt.wantBadge)
			}
			gotShort := tt.status.ShortBadge(tt.maxWidth)
			if gotShort != tt.wantShort {
				t.Errorf("ShortBadge(%d) = %q, want %q", tt.maxWidth, gotShort, tt.wantShort)
			}
		})
	}
}

func TestInspectRepoAndWorktree(t *testing.T) {
	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "my-repo")
	_ = os.MkdirAll(repoDir, 0o755)

	// Initialize real git repo
	cmd := exec.Command("git", "init", repoDir)
	if err := cmd.Run(); err != nil {
		t.Skip("git not available for init test")
	}
	_ = exec.Command("git", "-C", repoDir, "config", "user.email", "test@example.com").Run()
	_ = exec.Command("git", "-C", repoDir, "config", "user.name", "Test").Run()

	ctx := context.Background()
	st := InspectRepo(ctx, "my-repo", repoDir)
	if !st.IsCloned {
		t.Fatalf("expected IsCloned=true")
	}
	if st.IsDirty {
		t.Errorf("expected clean repo, got IsDirty=true")
	}

	// Create an untracked file -> dirty
	_ = os.WriteFile(filepath.Join(repoDir, "newfile.txt"), []byte("hello"), 0o644)
	stDirty := InspectRepo(ctx, "my-repo", repoDir)
	if !stDirty.IsDirty {
		t.Errorf("expected IsDirty=true after adding untracked file")
	}
	if stDirty.State != StateDirty {
		t.Errorf("expected StateDirty, got %s", stDirty.State)
	}

	// Worktree simulation: a folder where .git is a file instead of a directory
	worktreeDir := filepath.Join(tmpDir, "worktree-repo")
	_ = os.MkdirAll(worktreeDir, 0o755)
	_ = os.WriteFile(filepath.Join(worktreeDir, ".git"), []byte("gitdir: /path/to/gitdir"), 0o644)

	stWorktree := InspectRepo(ctx, "worktree-repo", worktreeDir)
	if !stWorktree.IsCloned {
		t.Errorf("expected IsCloned=true for git worktree with .git file")
	}
}

func TestScanWorkspacesMergingAndSubfolders(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewWorkspaceManager(tmpDir)

	// Create repo 1 in root: ~/workspaces/repo-one/.git
	repo1Dir := filepath.Join(tmpDir, "repo-one", ".git")
	_ = os.MkdirAll(repo1Dir, 0o755)

	// Create repo 2 in subfolder: ~/workspaces/corp-org/repo-two/.git
	repo2Dir := filepath.Join(tmpDir, "corp-org", "repo-two", ".git")
	_ = os.MkdirAll(repo2Dir, 0o755)

	ctx := context.Background()

	// 1. Scan only repo-one
	statuses1 := mgr.ScanWorkspaces(ctx, []string{"repo-one"})
	if st, ok := statuses1["repo-one"]; !ok || !st.IsCloned {
		t.Fatalf("expected repo-one to be cloned in first scan")
	}

	// 2. Scan only repo-two (simulating single repo refresh via [r])
	statuses2 := mgr.ScanWorkspaces(ctx, []string{"corp-org/repo-two"})
	if st, ok := statuses2["corp-org/repo-two"]; !ok || !st.IsCloned {
		t.Fatalf("expected corp-org/repo-two to be cloned in second scan")
	}

	// Crucial check: repo-one MUST still be present in statuses2 and in cache!
	if st, ok := statuses2["repo-one"]; !ok || !st.IsCloned {
		t.Errorf("CRITICAL BUG: repo-one was wiped out from cache when refreshing repo-two!")
	}

	// Check GetStatus returns cloned for both
	if !mgr.GetStatus(ctx, "repo-one").IsCloned {
		t.Errorf("expected GetStatus(repo-one) to be cloned")
	}
	if !mgr.GetStatus(ctx, "repo-two").IsCloned {
		t.Errorf("expected GetStatus(repo-two) short name to be cloned")
	}
}

func TestScanWorkspacesLargeMixedWorkspaceAndDirtyIndexModTime(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewWorkspaceManager(tmpDir)

	// Initialize one real dirty git repo
	dirtyDir := filepath.Join(tmpDir, "dirty-svc")
	_ = os.MkdirAll(dirtyDir, 0o755)
	runGit := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dirtyDir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v (%s)", args, err, string(out))
		}
	}
	runGit("init", "-b", "main")
	runGit("config", "user.email", "test@example.com")
	runGit("config", "user.name", "Test User")
	_ = os.WriteFile(filepath.Join(dirtyDir, "README.md"), []byte("hello"), 0o644)
	runGit("add", ".")
	runGit("commit", "-m", "init")
	// Make dirty untracked file
	_ = os.WriteFile(filepath.Join(dirtyDir, "untracked.txt"), []byte("dirty"), 0o644)

	known := make([]string, 0, 150)
	known = append(known, "dirty-svc")
	for i := 1; i < 150; i++ {
		known = append(known, fmt.Sprintf("uncloned-repo-%03d", i))
	}

	statuses := mgr.ScanWorkspaces(context.Background(), known)
	if len(statuses) < 150 {
		t.Fatalf("expected at least 150 statuses, got %d", len(statuses))
	}
	st := statuses["dirty-svc"]
	if !st.IsCloned || !st.IsDirty {
		t.Fatalf("expected dirty-svc to be cloned and dirty, got %+v", st)
	}
	if st.IndexModTime.IsZero() {
		t.Errorf("expected IndexModTime to be populated for dirty repo during ScanWorkspaces")
	}
	uncloned := statuses["uncloned-repo-149"]
	if uncloned.IsCloned || uncloned.State != StateNotCloned {
		t.Errorf("expected uncloned repo to be StateNotCloned, got %+v", uncloned)
	}
}
