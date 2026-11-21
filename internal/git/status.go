package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"
)

type GitState string

const (
	StateNotCloned  GitState = "NOT_CLONED"
	StateSynced     GitState = "SYNCED"
	StateBehind     GitState = "BEHIND"
	StateAhead      GitState = "AHEAD"
	StateDiverged   GitState = "DIVERGED"
	StateDirty      GitState = "DIRTY"
	StateNoUpstream GitState = "NO_UPSTREAM"
)

type RepoGitStatus struct {
	RepoName    string
	LocalPath   string
	IsCloned    bool
	Branch      string
	IsDirty     bool
	Ahead       int
	Behind      int
	State       GitState
	HasUpstream bool
}

func (s RepoGitStatus) Badge() string {
	if !s.IsCloned {
		return "[Not Cloned]"
	}

	branch := s.Branch
	if branch == "" {
		branch = "main"
	}

	var syncParts []string
	if s.Ahead > 0 {
		syncParts = append(syncParts, fmt.Sprintf("↑%d", s.Ahead))
	}
	if s.Behind > 0 {
		syncParts = append(syncParts, fmt.Sprintf("↓%d", s.Behind))
	}
	syncInfo := strings.Join(syncParts, " ")

	if s.IsDirty {
		if syncInfo != "" {
			return fmt.Sprintf("* %s %s", branch, syncInfo)
		}
		return fmt.Sprintf("* %s", branch)
	}

	switch s.State {
	case StateBehind:
		return fmt.Sprintf("↓%d %s", s.Behind, branch)
	case StateAhead:
		return fmt.Sprintf("↑%d %s", s.Ahead, branch)
	case StateDiverged:
		return fmt.Sprintf("⇡%d ⇣%d %s", s.Ahead, s.Behind, branch)
	case StateNoUpstream:
		return fmt.Sprintf("? %s", branch)
	case StateSynced:
		return fmt.Sprintf("✓ %s", branch)
	default:
		if syncInfo != "" {
			return fmt.Sprintf("%s %s", branch, syncInfo)
		}
		return fmt.Sprintf("✓ %s", branch)
	}
}

// ShortBadge returns the badge formatted and truncated to fit within maxWidth columns
func (s RepoGitStatus) ShortBadge(maxWidth int) string {
	full := s.Badge()
	if maxWidth <= 0 || runewidth.StringWidth(full) <= maxWidth {
		return full
	}

	if !s.IsCloned {
		return "[Not Cloned]"
	}

	branch := s.Branch
	if branch == "" {
		branch = "main"
	}

	var syncParts []string
	if s.Ahead > 0 {
		syncParts = append(syncParts, fmt.Sprintf("↑%d", s.Ahead))
	}
	if s.Behind > 0 {
		syncParts = append(syncParts, fmt.Sprintf("↓%d", s.Behind))
	}
	syncInfo := strings.Join(syncParts, " ")

	var prefix, suffix string
	if s.IsDirty {
		prefix = "* "
		if syncInfo != "" {
			suffix = " " + syncInfo
		}
	} else if s.Ahead > 0 && s.Behind > 0 {
		prefix = fmt.Sprintf("⇡%d ⇣%d ", s.Ahead, s.Behind)
	} else if s.Ahead > 0 {
		prefix = fmt.Sprintf("↑%d ", s.Ahead)
	} else if s.Behind > 0 {
		prefix = fmt.Sprintf("↓%d ", s.Behind)
	} else if !s.HasUpstream || s.State == StateNoUpstream {
		prefix = "? "
	} else {
		prefix = "✓ "
	}

	prefixW := runewidth.StringWidth(prefix)
	suffixW := runewidth.StringWidth(suffix)
	avail := maxWidth - prefixW - suffixW
	if avail < 3 {
		if s.IsDirty {
			if syncInfo != "" && runewidth.StringWidth("* "+syncInfo) <= maxWidth {
				return "* " + syncInfo
			}
			return "* dirty"
		}
		return runewidth.Truncate(full, maxWidth, "…")
	}

	shortBranch := runewidth.Truncate(branch, avail, "…")
	return prefix + shortBranch + suffix
}

// InspectRepo returns the git status of a local directory
func InspectRepo(ctx context.Context, repoName, dir string) RepoGitStatus {
	st := RepoGitStatus{
		RepoName:  repoName,
		LocalPath: dir,
		IsCloned:  false,
		State:     StateNotCloned,
	}

	gitDir := filepath.Join(dir, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		return st
	}
	st.IsCloned = true

	// Check branch
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD")
	if out, err := cmd.Output(); err == nil {
		branch := strings.TrimSpace(string(out))
		if branch == "HEAD" {
			// Detached HEAD: resolve to short sha or tag
			shaCmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--short", "HEAD")
			if shaOut, err := shaCmd.Output(); err == nil {
				st.Branch = "@" + strings.TrimSpace(string(shaOut))
			} else {
				st.Branch = "detached"
			}
		} else {
			st.Branch = branch
		}
	} else {
		st.Branch = "main"
	}

	// Check dirty status
	cmd = exec.CommandContext(ctx, "git", "-C", dir, "status", "--porcelain")
	if out, err := cmd.Output(); err == nil {
		if len(strings.TrimSpace(string(out))) > 0 {
			st.IsDirty = true
		}
	}

	// Check ahead/behind tracking branch
	cmd = exec.CommandContext(ctx, "git", "-C", dir, "rev-list", "--left-right", "--count", "HEAD...@{u}")
	if out, err := cmd.Output(); err == nil {
		parts := strings.Fields(strings.TrimSpace(string(out)))
		if len(parts) == 2 {
			st.Ahead, _ = strconv.Atoi(parts[0])
			st.Behind, _ = strconv.Atoi(parts[1])
			st.HasUpstream = true
		}
	} else {
		// No upstream branch configured
		st.HasUpstream = false
		st.State = StateNoUpstream
		if st.IsDirty {
			st.State = StateDirty
		}
		return st
	}

	// Compute state
	if st.IsDirty {
		st.State = StateDirty
	} else if st.Ahead > 0 && st.Behind > 0 {
		st.State = StateDiverged
	} else if st.Behind > 0 {
		st.State = StateBehind
	} else if st.Ahead > 0 {
		st.State = StateAhead
	} else {
		st.State = StateSynced
	}

	return st
}

// FastForward safely fast-forwards a repository
func FastForward(ctx context.Context, dir string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "merge", "--ff-only")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(out))
	}
	return nil
}

// BackgroundFetch fetches latest objects non-destructively
func BackgroundFetch(ctx context.Context, dir string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "fetch", "--all", "--prune")
	return cmd.Run()
}
