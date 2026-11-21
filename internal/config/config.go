package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

type SquadConfig struct {
	Name          string   `yaml:"name"`
	DisplayName   string   `yaml:"display_name"`
	GithubTeam    string   `yaml:"github_team,omitempty"`
	Members       []string `yaml:"members"`
	Organizations []string `yaml:"organizations,omitempty"`
	Repos         []string `yaml:"repos,omitempty"`
}

type GroupConfig struct {
	Name        string   `yaml:"name"`
	DisplayName string   `yaml:"display_name"`
	Repos       []string `yaml:"repos"`
}

type SearchConfig struct {
	Engine     string   `yaml:"engine"` // "ripgrep"
	IgnoreDirs []string `yaml:"ignore_dirs"`
}

type WebhookConfig struct {
	Enabled bool   `yaml:"enabled"`
	Port    int    `yaml:"port"` // Default: 8484
	Path    string `yaml:"path"` // Default: "/webhooks/github"
}

type Config struct {
	Version            int                  `yaml:"version"`
	DefaultOrg         string               `yaml:"default_org"` // Fallback / legacy
	HomeOrg            string               `yaml:"home_org,omitempty"`
	TrackedOrgs        []string             `yaml:"tracked_orgs,omitempty"`
	Webhook            WebhookConfig        `yaml:"webhook,omitempty"`
	WorkspaceDir       string               `yaml:"workspace_dir"`
	Editor             string               `yaml:"editor"`
	ActiveSquad        string               `yaml:"active_squad"`
	Squads             []SquadConfig        `yaml:"squads"`
	Groups             []GroupConfig        `yaml:"groups"`
	Search             SearchConfig         `yaml:"search"`
	RepoInteractions   map[string]time.Time `yaml:"repo_interactions,omitempty"`
	Proxy              string               `yaml:"proxy,omitempty"`
	InsecureSkipVerify bool                 `yaml:"insecure_skip_verify,omitempty"`
	HiddenRepos        []string             `yaml:"hidden_repos,omitempty"`
	CentralTasksRepo   string               `yaml:"central_tasks_repo,omitempty"`

	filePath string     `yaml:"-"`
	mu       sync.Mutex `yaml:"-"`
}

// DefaultConfigPath returns ~/.config/grubby/config.yaml
func DefaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "grubby", "config.yaml"), nil
}

// Exists returns true if the configuration file exists at path (or default path)
func Exists(path string) bool {
	if path == "" {
		var err error
		path, err = DefaultConfigPath()
		if err != nil {
			return false
		}
	}
	path = ExpandHome(path)
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

// ExpandHome expands leading ~ in file paths
func ExpandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// CollapseHome replaces the user's home directory path with ~/
func CollapseHome(path string) string {
	home, err := os.UserHomeDir()
	if err == nil && home != "" && strings.HasPrefix(path, home) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

// Load loads or creates the config at path
func Load(path string) (*Config, error) {
	if path == "" {
		var err error
		path, err = DefaultConfigPath()
		if err != nil {
			return nil, err
		}
	}
	path = ExpandHome(path)

	cfg := &Config{
		Version:      1,
		WorkspaceDir: "~/workspaces",
		Editor:       os.Getenv("EDITOR"),
		Webhook: WebhookConfig{
			Enabled: true,
			Port:    8484,
			Path:    "/webhooks/github",
		},
		Search: SearchConfig{
			Engine: "ripgrep",
			IgnoreDirs: []string{
				".git", "node_modules", "vendor", "dist", "build", ".next",
			},
		},
		filePath: path,
	}
	if cfg.Editor == "" {
		cfg.Editor = "nvim"
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Create directory and initial file
			if err := cfg.Save(); err != nil {
				return nil, fmt.Errorf("failed to initialize config file: %w", err)
			}
			return cfg, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("invalid config format in %s: %w", path, err)
	}
	if cfg.Webhook.Port == 0 {
		cfg.Webhook.Port = 8484
	}
	if cfg.Webhook.Path == "" {
		cfg.Webhook.Path = "/webhooks/github"
	}
	if cfg.HomeOrg == "" && cfg.DefaultOrg != "" {
		cfg.HomeOrg = cfg.DefaultOrg
	}
	cfg.filePath = path
	return cfg, nil
}

// Save persists the config atomically to disk
func (c *Config) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.filePath == "" {
		return nil
	}

	dir := filepath.Dir(c.filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	tmpFile := c.filePath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0o644); err != nil {
		return fmt.Errorf("failed to write tmp config: %w", err)
	}

	if err := os.Rename(tmpFile, c.filePath); err != nil {
		return fmt.Errorf("failed to atomically replace config: %w", err)
	}
	return nil
}

// Group Helpers

func (c *Config) GetGroupsForRepo(repoName string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	var matched []string
	for _, g := range c.Groups {
		for _, r := range g.Repos {
			if strings.EqualFold(r, repoName) {
				matched = append(matched, g.Name)
				break
			}
		}
	}
	return matched
}

func (c *Config) GetAllGroupNames() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	var names []string
	for _, g := range c.Groups {
		names = append(names, g.Name)
	}
	return names
}

func (c *Config) IsRepoInGroup(groupName, repoName string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, g := range c.Groups {
		if strings.EqualFold(g.Name, groupName) {
			for _, r := range g.Repos {
				if strings.EqualFold(r, repoName) {
					return true
				}
			}
			return false
		}
	}
	return false
}

func (c *Config) GetGroup(groupName string) *GroupConfig {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i := range c.Groups {
		if strings.EqualFold(c.Groups[i].Name, groupName) {
			return &c.Groups[i]
		}
	}
	return nil
}

func (c *Config) ToggleRepoInGroup(repoName, groupName string) (bool, error) {
	c.mu.Lock()
	groupIndex := -1
	for i, g := range c.Groups {
		if strings.EqualFold(g.Name, groupName) {
			groupIndex = i
			break
		}
	}

	if groupIndex == -1 {
		// Create new group
		c.Groups = append(c.Groups, GroupConfig{
			Name:        groupName,
			DisplayName: groupName,
			Repos:       []string{repoName},
		})
		c.mu.Unlock()
		return true, c.Save()
	}

	inGroup := false
	var updatedRepos []string
	for _, r := range c.Groups[groupIndex].Repos {
		if strings.EqualFold(r, repoName) {
			inGroup = true
		} else {
			updatedRepos = append(updatedRepos, r)
		}
	}

	if inGroup {
		c.Groups[groupIndex].Repos = updatedRepos
		c.mu.Unlock()
		return false, c.Save()
	}

	c.Groups[groupIndex].Repos = append(c.Groups[groupIndex].Repos, repoName)
	c.mu.Unlock()
	return true, c.Save()
}

// Squad Helpers

func (c *Config) GetActiveSquad() *SquadConfig {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.Squads) == 0 {
		return nil
	}
	for i := range c.Squads {
		if strings.EqualFold(c.Squads[i].Name, c.ActiveSquad) {
			return &c.Squads[i]
		}
	}
	// Fallback to first squad if active_squad is unset
	return &c.Squads[0]
}

func (c *Config) SetActiveSquad(squadName string) error {
	c.mu.Lock()
	c.ActiveSquad = squadName
	c.mu.Unlock()
	return c.Save()
}

func (c *Config) AddMemberToSquad(squadName, member string) error {
	c.mu.Lock()
	cleanMember := strings.TrimPrefix(strings.TrimSpace(member), "@")
	if cleanMember == "" {
		c.mu.Unlock()
		return fmt.Errorf("invalid member name")
	}

	for i := range c.Squads {
		if strings.EqualFold(c.Squads[i].Name, squadName) {
			for _, m := range c.Squads[i].Members {
				if strings.EqualFold(m, cleanMember) {
					c.mu.Unlock()
					return nil // Already member
				}
			}
			c.Squads[i].Members = append(c.Squads[i].Members, cleanMember)
			c.mu.Unlock()
			return c.Save()
		}
	}

	// Create squad if not found
	c.Squads = append(c.Squads, SquadConfig{
		Name:        squadName,
		DisplayName: squadName,
		Members:     []string{cleanMember},
	})
	if c.ActiveSquad == "" {
		c.ActiveSquad = squadName
	}
	c.mu.Unlock()
	return c.Save()
}

func (c *Config) RemoveMemberFromSquad(squadName, member string) error {
	c.mu.Lock()
	cleanMember := strings.TrimPrefix(strings.TrimSpace(member), "@")

	for i := range c.Squads {
		if strings.EqualFold(c.Squads[i].Name, squadName) {
			var updated []string
			for _, m := range c.Squads[i].Members {
				if !strings.EqualFold(m, cleanMember) {
					updated = append(updated, m)
				}
			}
			c.Squads[i].Members = updated
			c.mu.Unlock()
			return c.Save()
		}
	}
	c.mu.Unlock()
	return nil
}

func (c *Config) CreateSquad(name, displayName string, members []string) error {
	c.mu.Lock()
	cleanName := strings.TrimSpace(name)
	if cleanName == "" {
		c.mu.Unlock()
		return fmt.Errorf("squad name cannot be empty")
	}
	if displayName == "" {
		displayName = cleanName
	}
	for i := range c.Squads {
		if strings.EqualFold(c.Squads[i].Name, cleanName) {
			c.Squads[i].DisplayName = displayName
			c.Squads[i].Members = members
			c.mu.Unlock()
			return c.Save()
		}
	}
	c.Squads = append(c.Squads, SquadConfig{
		Name:        cleanName,
		DisplayName: displayName,
		Members:     members,
	})
	if c.ActiveSquad == "" {
		c.ActiveSquad = cleanName
	}
	c.mu.Unlock()
	return c.Save()
}

func (c *Config) RenameSquad(oldName, newName string) error {
	c.mu.Lock()

	cleanNew := strings.TrimSpace(newName)
	if cleanNew == "" {
		c.mu.Unlock()
		return fmt.Errorf("new squad name cannot be empty")
	}

	for i := range c.Squads {
		if strings.EqualFold(c.Squads[i].Name, oldName) {
			c.Squads[i].Name = cleanNew
			c.Squads[i].DisplayName = cleanNew
			if strings.EqualFold(c.ActiveSquad, oldName) {
				c.ActiveSquad = cleanNew
			}
			c.mu.Unlock()
			return c.Save()
		}
	}
	c.mu.Unlock()
	return fmt.Errorf("squad '%s' not found", oldName)
}

func (c *Config) DeleteSquad(squadName string) error {
	c.mu.Lock()

	foundIndex := -1
	for i := range c.Squads {
		if strings.EqualFold(c.Squads[i].Name, squadName) {
			foundIndex = i
			break
		}
	}

	if foundIndex == -1 {
		c.mu.Unlock()
		return fmt.Errorf("squad '%s' not found", squadName)
	}

	c.Squads = append(c.Squads[:foundIndex], c.Squads[foundIndex+1:]...)

	if strings.EqualFold(c.ActiveSquad, squadName) {
		if len(c.Squads) > 0 {
			c.ActiveSquad = c.Squads[0].Name
		} else {
			c.ActiveSquad = ""
		}
	}

	c.mu.Unlock()
	return c.Save()
}

func (c *Config) SetSquadMembers(squadName string, members []string) error {
	c.mu.Lock()

	for i := range c.Squads {
		if strings.EqualFold(c.Squads[i].Name, squadName) {
			c.Squads[i].Members = members
			c.mu.Unlock()
			return c.Save()
		}
	}
	c.mu.Unlock()
	return fmt.Errorf("squad '%s' not found", squadName)
}

// Squad Organization Helpers

func (c *Config) GetOrgsForSquad(squadName string) []string {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, s := range c.Squads {
		if strings.EqualFold(s.Name, squadName) {
			return append([]string{}, s.Organizations...)
		}
	}
	return nil
}

func (c *Config) AddOrgToSquad(squadName, org string) error {
	c.mu.Lock()
	cleanOrg := strings.TrimSpace(org)
	if cleanOrg == "" {
		c.mu.Unlock()
		return fmt.Errorf("organization name cannot be empty")
	}

	for i := range c.Squads {
		if strings.EqualFold(c.Squads[i].Name, squadName) {
			for _, o := range c.Squads[i].Organizations {
				if strings.EqualFold(o, cleanOrg) {
					c.mu.Unlock()
					return nil // already exists
				}
			}
			c.Squads[i].Organizations = append(c.Squads[i].Organizations, cleanOrg)
			c.mu.Unlock()
			return c.Save()
		}
	}
	c.mu.Unlock()
	return fmt.Errorf("squad '%s' not found", squadName)
}

func (c *Config) RemoveOrgFromSquad(squadName, org string) error {
	c.mu.Lock()
	cleanOrg := strings.TrimSpace(org)

	for i := range c.Squads {
		if strings.EqualFold(c.Squads[i].Name, squadName) {
			var updated []string
			for _, o := range c.Squads[i].Organizations {
				if !strings.EqualFold(o, cleanOrg) {
					updated = append(updated, o)
				}
			}
			c.Squads[i].Organizations = updated
			c.mu.Unlock()
			return c.Save()
		}
	}
	c.mu.Unlock()
	return fmt.Errorf("squad '%s' not found", squadName)
}

func (c *Config) SetSquadOrgs(squadName string, orgs []string) error {
	c.mu.Lock()

	for i := range c.Squads {
		if strings.EqualFold(c.Squads[i].Name, squadName) {
			var clean []string
			seen := make(map[string]bool)
			for _, o := range orgs {
				trimmed := strings.TrimSpace(o)
				if trimmed != "" && !seen[strings.ToLower(trimmed)] {
					seen[strings.ToLower(trimmed)] = true
					clean = append(clean, trimmed)
				}
			}
			c.Squads[i].Organizations = clean
			c.mu.Unlock()
			return c.Save()
		}
	}
	c.mu.Unlock()
	return fmt.Errorf("squad '%s' not found", squadName)
}

func (c *Config) GetActiveSquadOrgs() []string {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, s := range c.Squads {
		if strings.EqualFold(s.Name, c.ActiveSquad) {
			if len(s.Organizations) > 0 {
				var orgs []string
				seen := make(map[string]bool)
				for _, o := range s.Organizations {
					clean := strings.TrimSpace(o)
					if clean != "" && !seen[strings.ToLower(clean)] {
						seen[strings.ToLower(clean)] = true
						orgs = append(orgs, clean)
					}
				}
				if len(orgs) > 0 {
					return orgs
				}
			}
			break
		}
	}

	// Fallback to HomeOrg or DefaultOrg
	home := c.HomeOrg
	if home == "" {
		home = c.DefaultOrg
	}
	if home != "" {
		return []string{home}
	}
	return nil
}

// Squad Repo Ownership Helpers

func (c *Config) IsRepoOwnedBySquad(squadName, repoName string) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	cleanRepo := strings.ToLower(strings.TrimSpace(repoName))
	for _, s := range c.Squads {
		if strings.EqualFold(s.Name, squadName) {
			for _, r := range s.Repos {
				if strings.ToLower(strings.TrimSpace(r)) == cleanRepo {
					return true
				}
			}
			return false
		}
	}
	return false
}

func (c *Config) ToggleRepoOwnership(squadName, repoName string) (bool, error) {
	if c == nil {
		return false, nil
	}
	c.mu.Lock()

	cleanRepo := strings.ToLower(strings.TrimSpace(repoName))
	if cleanRepo == "" {
		c.mu.Unlock()
		return false, fmt.Errorf("invalid repository name")
	}

	squadIdx := -1
	for i := range c.Squads {
		if strings.EqualFold(c.Squads[i].Name, squadName) {
			squadIdx = i
			break
		}
	}

	if squadIdx == -1 {
		// Auto-create squad if it doesn't exist
		c.Squads = append(c.Squads, SquadConfig{
			Name:        squadName,
			DisplayName: squadName,
			Repos:       []string{cleanRepo},
		})
		c.mu.Unlock()
		return true, c.Save()
	}

	inSquad := false
	var updatedRepos []string
	for _, r := range c.Squads[squadIdx].Repos {
		if strings.ToLower(strings.TrimSpace(r)) == cleanRepo {
			inSquad = true
		} else {
			updatedRepos = append(updatedRepos, r)
		}
	}

	if inSquad {
		c.Squads[squadIdx].Repos = updatedRepos
		c.mu.Unlock()
		return false, c.Save()
	}

	c.Squads[squadIdx].Repos = append(c.Squads[squadIdx].Repos, cleanRepo)
	c.mu.Unlock()
	return true, c.Save()
}

func (c *Config) GetReposForSquad(squadName string) []string {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, s := range c.Squads {
		if strings.EqualFold(s.Name, squadName) {
			res := make([]string, len(s.Repos))
			copy(res, s.Repos)
			return res
		}
	}
	return nil
}

func (c *Config) GetSquadsForRepo(repoName string) []string {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	cleanRepo := strings.ToLower(strings.TrimSpace(repoName))
	var squads []string
	for _, s := range c.Squads {
		for _, r := range s.Repos {
			if strings.ToLower(strings.TrimSpace(r)) == cleanRepo {
				squads = append(squads, s.Name)
				break
			}
		}
	}
	return squads
}

func (c *Config) SetSquadRepos(squadName string, repos []string) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()

	for i := range c.Squads {
		if strings.EqualFold(c.Squads[i].Name, squadName) {
			c.Squads[i].Repos = repos
			c.mu.Unlock()
			return c.Save()
		}
	}
	c.mu.Unlock()
	return fmt.Errorf("squad '%s' not found", squadName)
}

// Repo Interaction Helpers

// RecordRepoInteraction stores the current timestamp for a repository action and persists the config.
func (c *Config) RecordRepoInteraction(repoName string) error {
	c.mu.Lock()
	if c.RepoInteractions == nil {
		c.RepoInteractions = make(map[string]time.Time)
	}
	c.RepoInteractions[strings.ToLower(strings.TrimSpace(repoName))] = time.Now()
	c.mu.Unlock()
	return c.Save()
}

// GetRepoLastInteracted returns the recorded last interaction time for a repository.
func (c *Config) GetRepoLastInteracted(repoName string) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.RepoInteractions == nil {
		return time.Time{}
	}
	return c.RepoInteractions[strings.ToLower(strings.TrimSpace(repoName))]
}

// Hidden Repositories Helpers

// IsRepoHidden returns true if repoName is marked hidden in the config.
func (c *Config) IsRepoHidden(repoName string) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	clean := strings.ToLower(strings.TrimSpace(repoName))
	if clean == "" {
		return false
	}
	short := clean
	if idx := strings.LastIndex(clean, "/"); idx != -1 && idx < len(clean)-1 {
		short = clean[idx+1:]
	}
	for _, r := range c.HiddenRepos {
		rClean := strings.ToLower(strings.TrimSpace(r))
		rShort := rClean
		if idx := strings.LastIndex(rClean, "/"); idx != -1 && idx < len(rClean)-1 {
			rShort = rClean[idx+1:]
		}
		if rClean == clean || rShort == short || rClean == short || rShort == clean {
			return true
		}
	}
	return false
}

// SetRepoHidden marks or unmarks a repository as hidden and persists the config.
func (c *Config) SetRepoHidden(repoName string, hidden bool) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	clean := strings.ToLower(strings.TrimSpace(repoName))
	short := clean
	if idx := strings.LastIndex(clean, "/"); idx != -1 && idx < len(clean)-1 {
		short = clean[idx+1:]
	}
	var updated []string
	for _, r := range c.HiddenRepos {
		rClean := strings.ToLower(strings.TrimSpace(r))
		rShort := rClean
		if idx := strings.LastIndex(rClean, "/"); idx != -1 && idx < len(rClean)-1 {
			rShort = rClean[idx+1:]
		}
		if rClean == clean || rShort == short || rClean == short || rShort == clean {
			continue
		}
		updated = append(updated, r)
	}
	if hidden {
		updated = append(updated, repoName)
	}
	c.HiddenRepos = updated
	c.mu.Unlock()
	return c.Save()
}

// ToggleRepoHidden toggles the hidden state of a repository and persists the config.
func (c *Config) ToggleRepoHidden(repoName string) (bool, error) {
	if c == nil {
		return false, nil
	}
	nowHidden := !c.IsRepoHidden(repoName)
	err := c.SetRepoHidden(repoName, nowHidden)
	return nowHidden, err
}

// GetHomeOrg returns HomeOrg if set, otherwise falls back to DefaultOrg.
func (c *Config) GetHomeOrg() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.HomeOrg != "" {
		return c.HomeOrg
	}
	return c.DefaultOrg
}

// SetHomeOrg sets the primary home organization and saves config.
func (c *Config) SetHomeOrg(org string) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	c.HomeOrg = strings.TrimSpace(org)
	if c.DefaultOrg == "" {
		c.DefaultOrg = c.HomeOrg
	}
	c.mu.Unlock()
	return c.Save()
}

// GetTrackedOrgs returns all configured tracked organizations.
// It includes HomeOrg, DefaultOrg, active squad organizations, and any explicit TrackedOrgs.
func (c *Config) GetTrackedOrgs() []string {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	seen := make(map[string]bool)
	var result []string

	if c.HomeOrg != "" {
		seen[strings.ToLower(c.HomeOrg)] = true
		result = append(result, c.HomeOrg)
	}
	if c.DefaultOrg != "" && !seen[strings.ToLower(c.DefaultOrg)] {
		seen[strings.ToLower(c.DefaultOrg)] = true
		result = append(result, c.DefaultOrg)
	}

	for _, s := range c.Squads {
		if strings.EqualFold(s.Name, c.ActiveSquad) {
			for _, o := range s.Organizations {
				clean := strings.TrimSpace(o)
				if clean != "" && !seen[strings.ToLower(clean)] {
					seen[strings.ToLower(clean)] = true
					result = append(result, clean)
				}
			}
			break
		}
	}

	for _, o := range c.TrackedOrgs {
		clean := strings.TrimSpace(o)
		if clean != "" && !seen[strings.ToLower(clean)] {
			seen[strings.ToLower(clean)] = true
			result = append(result, clean)
		}
	}
	return result
}

// AddTrackedOrg adds an organization to tracked_orgs if not present and persists config.
func (c *Config) AddTrackedOrg(org string) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	clean := strings.TrimSpace(org)
	if clean == "" {
		c.mu.Unlock()
		return nil
	}
	for _, o := range c.TrackedOrgs {
		if strings.EqualFold(o, clean) {
			c.mu.Unlock()
			return nil
		}
	}
	c.TrackedOrgs = append(c.TrackedOrgs, clean)
	c.mu.Unlock()
	return c.Save()
}

// RemoveTrackedOrg removes an organization from tracked_orgs and persists config.
func (c *Config) RemoveTrackedOrg(org string) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	clean := strings.TrimSpace(org)
	var updated []string
	for _, o := range c.TrackedOrgs {
		if !strings.EqualFold(o, clean) {
			updated = append(updated, o)
		}
	}
	c.TrackedOrgs = updated
	c.mu.Unlock()
	return c.Save()
}

// Central Tasks Repository Helpers

// GetCentralTasksRepo resolves the owner and repo name for the central tasks repository.
// If central_tasks_repo is configured as "owner/repo", it returns (owner, repo).
// If configured as "repo", it returns (homeOrg or defaultOrg or c.DefaultOrg, repo).
// If unset, it defaults to (homeOrg or defaultOrg or c.DefaultOrg, "grubby-tasks").
func (c *Config) GetCentralTasksRepo(defaultOrg string) (owner, repo string) {
	if c == nil {
		if defaultOrg != "" {
			return defaultOrg, "grubby-tasks"
		}
		return "", "grubby-tasks"
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	tasksRepo := strings.TrimSpace(c.CentralTasksRepo)
	if tasksRepo == "" {
		org := c.HomeOrg
		if org == "" {
			org = defaultOrg
		}
		if org == "" {
			org = c.DefaultOrg
		}
		return org, "grubby-tasks"
	}

	parts := strings.Split(tasksRepo, "/")
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}

	org := c.HomeOrg
	if org == "" {
		org = defaultOrg
	}
	if org == "" {
		org = c.DefaultOrg
	}
	return org, strings.TrimSpace(parts[0])
}

// GetCentralTasksRepoFullName returns "owner/repo" or "repo" if owner is empty.
func (c *Config) GetCentralTasksRepoFullName(defaultOrg string) string {
	owner, repo := c.GetCentralTasksRepo(defaultOrg)
	if owner == "" {
		return repo
	}
	return owner + "/" + repo
}

// SetCentralTasksRepo configures the central tasks repository and persists the config.
func (c *Config) SetCentralTasksRepo(repoFullName string) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	c.CentralTasksRepo = strings.TrimSpace(repoFullName)
	c.mu.Unlock()
	return c.Save()
}


