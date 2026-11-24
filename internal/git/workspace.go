package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"grubby/internal/config"
)

type WorkspaceManager struct {
	workspaceDir string
	mu           sync.RWMutex
	statusCache  map[string]RepoGitStatus
}

func NewWorkspaceManager(workspaceDir string) *WorkspaceManager {
	expanded := config.ExpandHome(workspaceDir)
	return &WorkspaceManager{
		workspaceDir: expanded,
		statusCache:  make(map[string]RepoGitStatus),
	}
}

func (w *WorkspaceManager) WorkspaceDir() string {
	return w.workspaceDir
}

// GetRepoPath returns the expected local directory for a repo name
func (w *WorkspaceManager) GetRepoPath(repoName string) string {
	clean := strings.TrimSpace(repoName)
	if path, ok := w.FindRepoDir(clean); ok {
		return path
	}
	short := clean
	if idx := strings.LastIndex(clean, "/"); idx != -1 && idx < len(clean)-1 {
		short = clean[idx+1:]
	}
	return filepath.Join(w.workspaceDir, short)
}

// FindRepoDir checks if a local directory exists for a repository name (supporting short names,
// org/repo subpaths, worktree files, and normalized names).
func (w *WorkspaceManager) FindRepoDir(repoName string) (string, bool) {
	clean := strings.TrimSpace(repoName)
	if clean == "" {
		return "", false
	}
	short := clean
	if idx := strings.LastIndex(clean, "/"); idx != -1 && idx < len(clean)-1 {
		short = clean[idx+1:]
	}

	// 1. Direct check: workspaceDir/short
	shortPath := filepath.Join(w.workspaceDir, short)
	if isGitRepo(shortPath) {
		return shortPath, true
	}

	// 2. Direct check: workspaceDir/clean (in case org/repo subdirectory structure is used)
	fullPath := filepath.Join(w.workspaceDir, clean)
	if isGitRepo(fullPath) {
		return fullPath, true
	}

	// 3. Scan workspace directory entries (case-insensitive & subfolder support)
	entries, err := os.ReadDir(w.workspaceDir)
	if err != nil {
		return "", false
	}

	lowerShort := strings.ToLower(short)
	lowerClean := strings.ToLower(clean)
	normShort := strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(lowerShort, ".git"), "-main"))

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		entryLower := strings.ToLower(entry.Name())
		dirPath := filepath.Join(w.workspaceDir, entry.Name())

		if isGitRepo(dirPath) {
			if entryLower == lowerShort || entryLower == lowerClean || entryLower == normShort {
				return dirPath, true
			}
		} else {
			// Check one level of subdirectories (e.g. ~/workspaces/org/repo)
			if subEntries, err := os.ReadDir(dirPath); err == nil {
				for _, sub := range subEntries {
					if sub.IsDir() {
						subPath := filepath.Join(dirPath, sub.Name())
						if isGitRepo(subPath) {
							subLower := strings.ToLower(sub.Name())
							combined := strings.ToLower(entry.Name() + "/" + sub.Name())
							if subLower == lowerShort || combined == lowerClean {
								return subPath, true
							}
						}
					}
				}
			}
		}
	}

	return "", false
}

// isGitRepo checks if dir has a .git directory or a .git worktree file
func isGitRepo(dir string) bool {
	gitPath := filepath.Join(dir, ".git")
	_, err := os.Stat(gitPath)
	return err == nil
}

// ScanWorkspaces inspects all local directories in the workspace folder concurrently
func (w *WorkspaceManager) ScanWorkspaces(ctx context.Context, knownRepos []string) map[string]RepoGitStatus {
	w.mu.Lock()
	if w.statusCache == nil {
		w.statusCache = make(map[string]RepoGitStatus)
	}
	w.mu.Unlock()

	entries, err := os.ReadDir(w.workspaceDir)
	if err != nil {
		w.mu.RLock()
		defer w.mu.RUnlock()
		copyMap := make(map[string]RepoGitStatus, len(w.statusCache))
		for k, v := range w.statusCache {
			copyMap[k] = v
		}
		return copyMap
	}

	clonedDirs := make(map[string]string)
	addCloned := func(name, fullPath string) {
		clean := strings.ToLower(strings.TrimSpace(name))
		clonedDirs[clean] = fullPath
		if strings.Contains(clean, "/") {
			short := clean[strings.LastIndex(clean, "/")+1:]
			clonedDirs[short] = fullPath
		}
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirPath := filepath.Join(w.workspaceDir, entry.Name())
		if isGitRepo(dirPath) {
			addCloned(entry.Name(), dirPath)
		} else {
			// Check one subfolder level (e.g. ~/workspaces/org-name/repo-name)
			if subEntries, err := os.ReadDir(dirPath); err == nil {
				for _, sub := range subEntries {
					if sub.IsDir() {
						subPath := filepath.Join(dirPath, sub.Name())
						if isGitRepo(subPath) {
							addCloned(sub.Name(), subPath)
							addCloned(entry.Name()+"/"+sub.Name(), subPath)
						}
					}
				}
			}
		}
	}

	findDir := func(name string) (string, bool) {
		clean := strings.ToLower(strings.TrimSpace(name))
		if p, ok := clonedDirs[clean]; ok {
			return p, true
		}
		if strings.Contains(clean, "/") {
			short := clean[strings.LastIndex(clean, "/")+1:]
			if p, ok := clonedDirs[short]; ok {
				return p, true
			}
			clean = short
		}
		norm := strings.TrimSuffix(strings.TrimSuffix(clean, ".git"), "-main")
		if p, ok := clonedDirs[norm]; ok {
			return p, true
		}
		return "", false
	}

	targets := knownRepos
	if len(targets) == 0 {
		seen := make(map[string]bool)
		for name := range clonedDirs {
			if !seen[name] {
				seen[name] = true
				targets = append(targets, name)
			}
		}
	}

	results := make(map[string]RepoGitStatus, len(targets))
	var toInspect []struct {
		repo string
		path string
	}

	for _, repo := range targets {
		if path, found := findDir(repo); found {
			toInspect = append(toInspect, struct {
				repo string
				path string
			}{repo: repo, path: path})
		} else {
			st := RepoGitStatus{
				RepoName:  repo,
				LocalPath: filepath.Join(w.workspaceDir, repo),
				IsCloned:  false,
				State:     StateNotCloned,
			}
			results[repo] = st
			if strings.Contains(repo, "/") {
				short := repo[strings.LastIndex(repo, "/")+1:]
				results[short] = st
			}
		}
	}

	if len(toInspect) > 0 {
		const maxWorkers = 4
		sem := make(chan struct{}, maxWorkers)
		var mu sync.Mutex
		var wg sync.WaitGroup

		for _, item := range toInspect {
			item := item
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				st := InspectRepo(ctx, item.repo, item.path)
				mu.Lock()
				results[item.repo] = st
				if strings.Contains(item.repo, "/") {
					short := item.repo[strings.LastIndex(item.repo, "/")+1:]
					results[short] = st
				}
				mu.Unlock()
			}()
		}
		wg.Wait()
	}

	// Merge results into w.statusCache (preserving previously known repositories)
	w.mu.Lock()
	for k, v := range results {
		w.statusCache[k] = v
	}
	combined := make(map[string]RepoGitStatus, len(w.statusCache))
	for k, v := range w.statusCache {
		combined[k] = v
	}
	w.mu.Unlock()

	return combined
}

// GetStatus returns the cached or freshly inspected status for a repo
func (w *WorkspaceManager) GetStatus(ctx context.Context, repoName string) RepoGitStatus {
	w.mu.RLock()
	st, ok := w.statusCache[repoName]
	if !ok && strings.Contains(repoName, "/") {
		short := repoName[strings.LastIndex(repoName, "/")+1:]
		st, ok = w.statusCache[short]
	}
	if !ok {
		st, ok = w.statusCache[strings.ToLower(repoName)]
	}
	w.mu.RUnlock()
	if ok && st.IsCloned {
		return st
	}

	if path, found := w.FindRepoDir(repoName); found {
		st = InspectRepo(ctx, repoName, path)
		w.mu.Lock()
		w.statusCache[repoName] = st
		if strings.Contains(repoName, "/") {
			short := repoName[strings.LastIndex(repoName, "/")+1:]
			w.statusCache[short] = st
		}
		w.mu.Unlock()
		return st
	}

	return RepoGitStatus{
		RepoName:  repoName,
		LocalPath: w.GetRepoPath(repoName),
		IsCloned:  false,
		State:     StateNotCloned,
	}
}

// ListClonedPaths returns all existing cloned directory paths in the workspace
func (w *WorkspaceManager) ListClonedPaths() []string {
	var paths []string
	entries, err := os.ReadDir(w.workspaceDir)
	if err != nil {
		return paths
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirPath := filepath.Join(w.workspaceDir, entry.Name())
		gitDir := filepath.Join(dirPath, ".git")
		if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
			paths = append(paths, dirPath)
		}
	}
	return paths
}
