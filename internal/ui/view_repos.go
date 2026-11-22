package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"grubby/internal/config"
	"grubby/internal/git"
	"grubby/internal/github"
	"grubby/internal/storage"
)

type RepoItemKind int

const (
	ItemTierHeader RepoItemKind = iota
	ItemRepo
	ItemRepoPR
)

type RepoTableItem struct {
	Kind       RepoItemKind
	Tier       storage.RepoTier
	Repo       *github.Repository
	PR         *github.PullRequest
	ChildCount int
	Expanded   bool
}

func (m Model) getPRsForRepo(repo *github.Repository) []github.PullRequest {
	if repo == nil {
		return nil
	}
	var res []github.PullRequest
	for _, pr := range m.PRs {
		if pr.RepoFullName != "" && repo.FullName != "" && strings.EqualFold(pr.RepoFullName, repo.FullName) {
			res = append(res, pr)
			continue
		}
		if pr.RepoOwner != "" && repo.Owner != "" && !strings.EqualFold(pr.RepoOwner, repo.Owner) {
			continue
		}
		if strings.EqualFold(github.NormalizeRepoName(pr.Repo), github.NormalizeRepoName(repo.Name)) {
			res = append(res, pr)
		}
	}
	return res
}

func (m Model) isRepoPRExpanded(r *github.Repository) bool {
	if r == nil || m.ExpandedRepoPRs == nil {
		return false
	}
	if r.FullName != "" && m.ExpandedRepoPRs[strings.ToLower(r.FullName)] {
		return true
	}
	return m.ExpandedRepoPRs[strings.ToLower(r.Name)]
}

func (m *Model) toggleRepoPRExpanded(r *github.Repository) {
	if r == nil {
		return
	}
	if m.ExpandedRepoPRs == nil {
		m.ExpandedRepoPRs = make(map[string]bool)
	}
	key := strings.ToLower(r.Name)
	if r.FullName != "" {
		key = strings.ToLower(r.FullName)
	}
	m.ExpandedRepoPRs[key] = !m.ExpandedRepoPRs[key]
}

func (m Model) getRepoLabels(r *github.Repository) []string {
	if r == nil {
		return nil
	}
	seen := make(map[string]bool)
	var result []string

	add := func(l string) {
		clean := strings.TrimPrefix(strings.TrimSpace(l), "#")
		clean = strings.ReplaceAll(clean, " ", "-")
		clean = strings.ToLower(clean)
		if clean != "" && !seen[clean] {
			seen[clean] = true
			result = append(result, clean)
		}
	}

	if r.ID > 0 && m.RepoLabels != nil {
		if labels, ok := m.RepoLabels[r.ID]; ok {
			for _, l := range labels {
				add(l)
			}
		}
	}
	if m.RepoLabelsByName != nil {
		if labels, ok := m.RepoLabelsByName[strings.ToLower(r.Name)]; ok {
			for _, l := range labels {
				add(l)
			}
		}
	}
	if m.Config != nil {
		for _, grp := range m.Config.GetGroupsForRepo(r.Name) {
			add(grp)
		}
	}

	return result
}

func (m Model) firstSelectableRepoIndex() int {
	items := m.getRepoTableItems()
	for idx, it := range items {
		if it.Kind == ItemRepo && it.Repo != nil {
			return idx
		}
	}
	return 0
}

func (m Model) focusRepoCursor(repoName string) Model {
	items := m.getRepoTableItems()
	for idx, it := range items {
		if it.Kind == ItemRepo && it.Repo != nil && strings.EqualFold(it.Repo.Name, repoName) {
			m.RepoCursor = idx
			break
		}
	}
	return m
}

func (m Model) getSelectedRepo() *github.Repository {
	items := m.getRepoTableItems()
	if len(items) > 0 && m.RepoCursor >= 0 && m.RepoCursor < len(items) {
		item := items[m.RepoCursor]
		if item.Kind == ItemRepo && item.Repo != nil {
			return item.Repo
		}
		if item.Kind == ItemRepoPR && item.Repo != nil {
			return item.Repo
		}
	}
	return nil
}

func (m Model) isTierCollapsed(tier storage.RepoTier) bool {
	// Auto-expand tiers when an active text search query ('/') or label filter ('#') is active
	// so that matching repositories across all tiers are visible to the user.
	// (Note: Squad ownership filter 'Shift-S' does NOT auto-expand; Dead and Hidden remain collapsed by default)
	if strings.TrimSpace(m.RepoFilter) != "" || strings.TrimSpace(m.RepoGroupFilter) != "" {
		return false
	}
	if m.CollapsedTiers == nil {
		return tier == storage.TierDead || tier == storage.TierHidden
	}
	collapsed, ok := m.CollapsedTiers[tier]
	if !ok {
		return tier == storage.TierDead || tier == storage.TierHidden
	}
	return collapsed
}

func (m Model) getRepoTableItems(reposOpt ...[]github.Repository) []RepoTableItem {
	var repos []github.Repository
	if len(reposOpt) > 0 && reposOpt[0] != nil {
		repos = reposOpt[0]
	} else {
		repos = m.getDisplayRepos()
	}
	if len(repos) == 0 {
		return nil
	}

	tierOrder := []storage.RepoTier{
		storage.TierHot,
		storage.TierWarm,
		storage.TierCold,
		storage.TierDead,
		storage.TierHidden,
	}

	tierRepos := make(map[storage.RepoTier][]github.Repository)
	for _, r := range repos {
		tier := m.getRepoTier(r.Name)
		tierRepos[tier] = append(tierRepos[tier], r)
	}

	var items []RepoTableItem
	for _, tier := range tierOrder {
		list := tierRepos[tier]
		if len(list) == 0 {
			continue
		}
		collapsed := m.isTierCollapsed(tier)
		items = append(items, RepoTableItem{
			Kind:       ItemTierHeader,
			Tier:       tier,
			ChildCount: len(list),
			Expanded:   !collapsed,
		})
		if !collapsed {
			for i := range list {
				r := &list[i]
				repoPRs := m.getPRsForRepo(r)
				isExpanded := m.isRepoPRExpanded(r)
				items = append(items, RepoTableItem{
					Kind:       ItemRepo,
					Tier:       tier,
					Repo:       r,
					ChildCount: len(repoPRs),
					Expanded:   isExpanded,
				})
				if isExpanded && len(repoPRs) > 0 {
					for j := range repoPRs {
						prCopy := repoPRs[j]
						items = append(items, RepoTableItem{
							Kind: ItemRepoPR,
							Tier: tier,
							Repo: r,
							PR:   &prCopy,
						})
					}
				}
			}
		}
	}

	return items
}

func (m Model) getDisplayRepos() []github.Repository {
	var result []github.Repository

	filter := strings.ToLower(strings.TrimSpace(m.RepoFilter))
	filter = strings.TrimPrefix(filter, "#")

	groupFilter := strings.ToLower(strings.TrimSpace(m.RepoGroupFilter))
	groupFilter = strings.TrimPrefix(groupFilter, "#")

	for _, r := range m.Repos {
		repoLabels := m.getRepoLabels(&r)

		// 1. Group / Label filter (from '#' modal)
		if groupFilter != "" {
			matchesGroup := false
			for _, lbl := range repoLabels {
				if strings.EqualFold(lbl, groupFilter) {
					matchesGroup = true
					break
				}
			}
			if !matchesGroup {
				continue
			}
		}

		// 2. Name or Label substring filter (from '/' modal)
		if filter != "" {
			matchesName := strings.Contains(strings.ToLower(r.Name), filter) ||
				strings.Contains(strings.ToLower(r.FullName), filter)

			matchesLabel := false
			for _, lbl := range repoLabels {
				if strings.Contains(strings.ToLower(lbl), filter) {
					matchesLabel = true
					break
				}
			}

			if !matchesName && !matchesLabel {
				continue
			}
		}

		// 3. Squad ownership filter (from Shift-S hotkey)
		if m.SquadFilterActive && m.Config != nil {
			activeSquad := m.Config.GetActiveSquad()
			if activeSquad != nil && !m.Config.IsRepoOwnedBySquad(activeSquad.Name, r.Name) {
				continue
			}
		}

		result = append(result, r)
	}

	effectiveSortMode := m.RepoSortMode
	if effectiveSortMode == RepoSortDefault && m.RepoSortVulnerabilities {
		effectiveSortMode = RepoSortVulns
	}

	switch effectiveSortMode {
	case RepoSortVulns:
		type vulnSortKey struct {
			total     int
			critical  int
			high      int
			medium    int
			lowerName string
		}
		keys := make(map[string]vulnSortKey, len(result))
		for _, r := range result {
			sec := m.SecuritySummaries[r.Name]
			keys[r.Name] = vulnSortKey{
				total:     sec.Total,
				critical:  sec.Critical,
				high:      sec.High,
				medium:    sec.Medium,
				lowerName: strings.ToLower(r.Name),
			}
		}

		sort.SliceStable(result, func(i, j int) bool {
			kA := keys[result[i].Name]
			kB := keys[result[j].Name]

			if kA.total != kB.total {
				return kA.total > kB.total
			}
			if kA.critical != kB.critical {
				return kA.critical > kB.critical
			}
			if kA.high != kB.high {
				return kA.high > kB.high
			}
			if kA.medium != kB.medium {
				return kA.medium > kB.medium
			}
			return kA.lowerName < kB.lowerName
		})

	case RepoSortPRs:
		type prSortKey struct {
			prCount     int
			lastUpdated time.Time
			lowerName   string
		}
		keys := make(map[string]prSortKey, len(result))
		for _, r := range result {
			prs := m.getPRsForRepo(&r)
			keys[r.Name] = prSortKey{
				prCount:     len(prs),
				lastUpdated: r.LastUpdated(),
				lowerName:   strings.ToLower(r.Name),
			}
		}

		sort.SliceStable(result, func(i, j int) bool {
			kA := keys[result[i].Name]
			kB := keys[result[j].Name]

			if kA.prCount != kB.prCount {
				return kA.prCount > kB.prCount
			}
			if !kA.lastUpdated.Equal(kB.lastUpdated) {
				return kA.lastUpdated.After(kB.lastUpdated)
			}
			return kA.lowerName < kB.lowerName
		})

	case RepoSortLastUpdated:
		type lastUpdatedSortKey struct {
			lastUpdated time.Time
			lowerName   string
		}
		keys := make(map[string]lastUpdatedSortKey, len(result))
		for _, r := range result {
			keys[r.Name] = lastUpdatedSortKey{
				lastUpdated: r.LastUpdated(),
				lowerName:   strings.ToLower(r.Name),
			}
		}

		sort.SliceStable(result, func(i, j int) bool {
			kA := keys[result[i].Name]
			kB := keys[result[j].Name]

			if !kA.lastUpdated.Equal(kB.lastUpdated) {
				return kA.lastUpdated.After(kB.lastUpdated)
			}
			return kA.lowerName < kB.lowerName
		})

	default:
		tmuxSessions := m.TmuxSessions
		if tmuxSessions == nil {
			tmuxSessions = GetTmuxSessions()
		}

		type repoSortKey struct {
			tierPriority int
			lastAct      time.Time
			lastUpdated  time.Time
			lowerName    string
		}
		keys := make(map[string]repoSortKey, len(result))
		for _, r := range result {
			keys[r.Name] = repoSortKey{
				tierPriority: storage.TierPriority(m.getRepoTier(r.Name)),
				lastAct:      m.getRepoLastInteractedTime(r.Name, tmuxSessions),
				lastUpdated:  r.LastUpdated(),
				lowerName:    strings.ToLower(r.Name),
			}
		}

		sort.SliceStable(result, func(i, j int) bool {
			kA := keys[result[i].Name]
			kB := keys[result[j].Name]

			// 0. Group by Tier priority (Hot=0, Warm=1, Cold=2, Dead=3)
			if kA.tierPriority != kB.tierPriority {
				return kA.tierPriority < kB.tierPriority
			}

			hasActA := !kA.lastAct.IsZero()
			hasActB := !kB.lastAct.IsZero()

			// 1. Repositories opened / interacted with come first, sorted by last interaction descending
			if hasActA && hasActB {
				if !kA.lastAct.Equal(kB.lastAct) {
					return kA.lastAct.After(kB.lastAct)
				}
			} else if hasActA != hasActB {
				return hasActA
			}

			// 2. Then by last updated / pushed descending
			if !kA.lastUpdated.Equal(kB.lastUpdated) {
				return kA.lastUpdated.After(kB.lastUpdated)
			}

			// 3. Alphabetical tiebreaker
			return kA.lowerName < kB.lowerName
		})
	}

	return result
}

func (m Model) toggleRepoOwnership(repoName string) (tea.Model, tea.Cmd) {
	if m.Config == nil || m.Config.ActiveSquad == "" {
		return m, func() tea.Msg { return StatusToastMsg("No active squad configured") }
	}
	activeSquad := m.Config.ActiveSquad
	nowOwned, err := m.Config.ToggleRepoOwnership(activeSquad, repoName)
	if err != nil {
		return m, func() tea.Msg { return StatusToastMsg("Error: " + err.Error()) }
	}
	if m.DB != nil {
		var squadDefs []github.SquadDefinition
		for _, s := range m.Config.Squads {
			squadDefs = append(squadDefs, github.SquadDefinition{
				Name:        s.Name,
				DisplayName: s.DisplayName,
				Members:     s.Members,
				Repos:       s.Repos,
			})
		}
		_ = m.DB.SaveSquads(squadDefs)
	}
	dispName := activeSquad
	if s := m.Config.GetActiveSquad(); s != nil && s.DisplayName != "" {
		dispName = s.DisplayName
	}
	toastText := fmt.Sprintf("✓ Assigned '%s' to squad '%s'", repoName, dispName)
	if !nowOwned {
		toastText = fmt.Sprintf("Removed '%s' from squad '%s'", repoName, dispName)
	}
	return m, tea.Batch(
		func() tea.Msg { return StatusToastMsg(toastText) },
		m.syncSquadsFileCmd(),
	)
}

func (m Model) handleReposKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// 0. If in a leader mode, process the second keystroke
	if m.RepoLeaderMode != RepoLeaderNone {
		leader := m.RepoLeaderMode
		m.RepoLeaderMode = RepoLeaderNone

		if msg.String() == "esc" {
			return m, nil
		}

		switch leader {
		case RepoLeaderFilter:
			switch msg.String() {
			case "n":
				m.ActiveModal = ModalFilterRepoName
				m.RepoFilterInput.SetValue(m.RepoFilter)
				m.RepoFilterInput.Focus()
				return m, textinput.Blink
			case "l":
				m.ActiveModal = ModalFilterGroup
				m.ModalCursor = 0
				return m, nil
			case "s":
				if m.Config == nil || m.Config.ActiveSquad == "" {
					return m, func() tea.Msg { return StatusToastMsg("No active squad selected") }
				}
				m.SquadFilterActive = !m.SquadFilterActive
				m.RepoCursor = m.firstSelectableRepoIndex()
				if m.SquadFilterActive {
					squadName := m.Config.ActiveSquad
					if s := m.Config.GetActiveSquad(); s != nil && s.DisplayName != "" {
						squadName = s.DisplayName
					}
					return m, func() tea.Msg { return StatusToastMsg(fmt.Sprintf("Filtered repos owned by squad '%s'", squadName)) }
				}
				return m, func() tea.Msg { return StatusToastMsg("Cleared squad repo filter") }
			default:
				return m, nil
			}

		case RepoLeaderSort:
			switch msg.String() {
			case "v":
				if m.RepoSortMode == RepoSortVulns {
					m.RepoSortMode = RepoSortDefault
					m.RepoSortVulnerabilities = false
					m.RepoCursor = m.firstSelectableRepoIndex()
					return m, func() tea.Msg { return StatusToastMsg("Reset repository sort order to default") }
				}
				m.RepoSortMode = RepoSortVulns
				m.RepoSortVulnerabilities = true
				m.RepoCursor = m.firstSelectableRepoIndex()
				return m, func() tea.Msg { return StatusToastMsg("Sorted repositories by vulnerabilities (most to least)") }
			case "p":
				if m.RepoSortMode == RepoSortPRs {
					m.RepoSortMode = RepoSortDefault
					m.RepoSortVulnerabilities = false
					m.RepoCursor = m.firstSelectableRepoIndex()
					return m, func() tea.Msg { return StatusToastMsg("Reset repository sort order to default") }
				}
				m.RepoSortMode = RepoSortPRs
				m.RepoSortVulnerabilities = false
				if m.ExpandedRepoPRs == nil {
					m.ExpandedRepoPRs = make(map[string]bool)
				}
				for i := range m.Repos {
					r := &m.Repos[i]
					if len(m.getPRsForRepo(r)) > 0 {
						key := strings.ToLower(r.Name)
						if r.FullName != "" {
							key = strings.ToLower(r.FullName)
						}
						m.ExpandedRepoPRs[key] = true
					}
				}
				m.RepoCursor = m.firstSelectableRepoIndex()
				return m, func() tea.Msg { return StatusToastMsg("Sorted repositories by open pull requests (most to least)") }
			case "l":
				if m.RepoSortMode == RepoSortLastUpdated {
					m.RepoSortMode = RepoSortDefault
					m.RepoSortVulnerabilities = false
					m.RepoCursor = m.firstSelectableRepoIndex()
					return m, func() tea.Msg { return StatusToastMsg("Reset repository sort order to default") }
				}
				m.RepoSortMode = RepoSortLastUpdated
				m.RepoSortVulnerabilities = false
				m.RepoCursor = m.firstSelectableRepoIndex()
				return m, func() tea.Msg { return StatusToastMsg("Sorted repositories by last updated (newest to oldest)") }
			default:
				return m, nil
			}

		case RepoLeaderOpen:
			repo := m.getSelectedRepo()
			if repo == nil {
				return m, nil
			}
			m = m.recordRepoInteraction(repo.Name)
			m = m.focusRepoCursor(repo.Name)

			switch msg.String() {
			case "w", "c", "enter", "o":
				_ = OpenBrowser(repo.URL)
				return m, tea.Batch(
					func() tea.Msg { return StatusToastMsg("Opened " + repo.FullName + " in dedicated browser") },
					m.fetchSingleRepoCmd(m.CurrentOrg, repo.Name),
				)
			case "e":
				st := m.getGitStatus(repo.Name)
				if st.IsCloned {
					msgStr, err := OpenRepoEditor(repo.Name, st.LocalPath)
					if err != nil {
						return m, func() tea.Msg { return StatusToastMsg("Failed to open editor: " + err.Error()) }
					}
					return m, tea.Batch(
						func() tea.Msg { return StatusToastMsg(msgStr) },
						m.fetchSingleRepoCmd(m.CurrentOrg, repo.Name),
					)
				}
				return m.startCloneWithAction(*repo, RepoActionEditor)
			case "s":
				st := m.getGitStatus(repo.Name)
				if st.IsCloned {
					msgStr, err := GoToRepoSession(repo.Name, st.LocalPath)
					if err != nil {
						return m, func() tea.Msg { return StatusToastMsg("Failed to switch session: " + err.Error()) }
					}
					return m, tea.Batch(
						func() tea.Msg { return StatusToastMsg(msgStr) },
						m.fetchSingleRepoCmd(m.CurrentOrg, repo.Name),
					)
				}
				return m.startCloneWithAction(*repo, RepoActionSession)
			case "p":
				url := fmt.Sprintf("https://github.com/%s/pulls", repo.FullName)
				_ = OpenBrowser(url)
				return m, func() tea.Msg { return StatusToastMsg(fmt.Sprintf("Opened %s Pull Requests in browser", repo.Name)) }
			case "i":
				url := fmt.Sprintf("https://github.com/%s/issues", repo.FullName)
				_ = OpenBrowser(url)
				return m, func() tea.Msg { return StatusToastMsg(fmt.Sprintf("Opened %s Issues in browser", repo.Name)) }
			case "a":
				url := fmt.Sprintf("https://github.com/%s/actions", repo.FullName)
				_ = OpenBrowser(url)
				return m, func() tea.Msg { return StatusToastMsg(fmt.Sprintf("Opened %s Actions in browser", repo.Name)) }
			case "d":
				url := fmt.Sprintf("https://github.com/%s/security/dependabot", repo.FullName)
				_ = OpenBrowser(url)
				return m, func() tea.Msg { return StatusToastMsg(fmt.Sprintf("Opened %s Security in browser", repo.Name)) }
			case "r":
				url := fmt.Sprintf("https://github.com/%s/releases", repo.FullName)
				_ = OpenBrowser(url)
				return m, func() tea.Msg { return StatusToastMsg(fmt.Sprintf("Opened %s Releases in browser", repo.Name)) }
			case "n":
				url := fmt.Sprintf("https://github.com/%s/pulse", repo.FullName)
				_ = OpenBrowser(url)
				return m, func() tea.Msg { return StatusToastMsg(fmt.Sprintf("Opened %s Insights in browser", repo.Name)) }
			case "t":
				url := fmt.Sprintf("https://github.com/%s/settings", repo.FullName)
				_ = OpenBrowser(url)
				return m, func() tea.Msg { return StatusToastMsg(fmt.Sprintf("Opened %s Settings in browser", repo.Name)) }
			default:
				return m, nil
			}

		case RepoLeaderYank:
			repo := m.getSelectedRepo()
			if repo == nil {
				return m, nil
			}
			m = m.recordRepoInteraction(repo.Name)
			m = m.focusRepoCursor(repo.Name)

			switch msg.String() {
			case "w", "c", "enter", "y":
				url := repo.URL
				if url == "" {
					url = fmt.Sprintf("https://github.com/%s", repo.FullName)
				}
				_ = CopyToClipboard(url)
				return m, func() tea.Msg { return StatusToastMsg(fmt.Sprintf("Copied %s URL to clipboard: %s", repo.Name, url)) }
			case "s":
				_ = CopyToClipboard(repo.Name)
				return m, func() tea.Msg { return StatusToastMsg(fmt.Sprintf("Copied session name '%s' to clipboard", repo.Name)) }
			case "p":
				url := fmt.Sprintf("https://github.com/%s/pulls", repo.FullName)
				_ = CopyToClipboard(url)
				return m, func() tea.Msg { return StatusToastMsg(fmt.Sprintf("Copied %s PRs URL to clipboard: %s", repo.Name, url)) }
			case "i":
				url := fmt.Sprintf("https://github.com/%s/issues", repo.FullName)
				_ = CopyToClipboard(url)
				return m, func() tea.Msg { return StatusToastMsg(fmt.Sprintf("Copied %s Issues URL to clipboard: %s", repo.Name, url)) }
			case "a":
				url := fmt.Sprintf("https://github.com/%s/actions", repo.FullName)
				_ = CopyToClipboard(url)
				return m, func() tea.Msg { return StatusToastMsg(fmt.Sprintf("Copied %s Actions URL to clipboard: %s", repo.Name, url)) }
			case "d":
				url := fmt.Sprintf("https://github.com/%s/security/dependabot", repo.FullName)
				_ = CopyToClipboard(url)
				return m, func() tea.Msg { return StatusToastMsg(fmt.Sprintf("Copied %s Security URL to clipboard: %s", repo.Name, url)) }
			case "r":
				url := fmt.Sprintf("https://github.com/%s/releases", repo.FullName)
				_ = CopyToClipboard(url)
				return m, func() tea.Msg { return StatusToastMsg(fmt.Sprintf("Copied %s Releases URL to clipboard: %s", repo.Name, url)) }
			case "n":
				url := fmt.Sprintf("https://github.com/%s/pulse", repo.FullName)
				_ = CopyToClipboard(url)
				return m, func() tea.Msg { return StatusToastMsg(fmt.Sprintf("Copied %s Insights URL to clipboard: %s", repo.Name, url)) }
			case "t":
				url := fmt.Sprintf("https://github.com/%s/settings", repo.FullName)
				_ = CopyToClipboard(url)
				return m, func() tea.Msg { return StatusToastMsg(fmt.Sprintf("Copied %s Settings URL to clipboard: %s", repo.Name, url)) }
			default:
				return m, nil
			}
		}
	}

	items := m.getRepoTableItems()

	switch msg.String() {
	case "up":
		if m.RepoCursor > 0 {
			m.RepoCursor--
		}
		return m, nil
	case "k":
		if len(items) > 0 && m.RepoCursor >= 0 && m.RepoCursor < len(items) && items[m.RepoCursor].Kind == ItemRepoPR {
			break // Fall through to ItemRepoPR handler for chec[k]out branch!
		}
		if m.RepoCursor > 0 {
			m.RepoCursor--
		}
		return m, nil
	case "down", "j":
		if m.RepoCursor < len(items)-1 {
			m.RepoCursor++
		}
		return m, nil
	case "pgdown", "ctrl+d":
		visibleCount := m.Height - 8
		if visibleCount < 5 {
			visibleCount = 10
		}
		m.RepoCursor += visibleCount
		if m.RepoCursor >= len(items) {
			m.RepoCursor = len(items) - 1
		}
		if m.RepoCursor < 0 {
			m.RepoCursor = 0
		}
		return m, nil
	case "pgup", "ctrl+u":
		visibleCount := m.Height - 8
		if visibleCount < 5 {
			visibleCount = 10
		}
		m.RepoCursor -= visibleCount
		if m.RepoCursor < 0 {
			m.RepoCursor = 0
		}
		return m, nil
	case "home":
		m.RepoCursor = 0
		return m, nil
	case "end", "G":
		if len(items) > 0 {
			m.RepoCursor = len(items) - 1
		}
		return m, nil
	case "f":
		m.RepoLeaderMode = RepoLeaderFilter
		return m, nil
	case "S":
		// 'S' (Shift-S): Sort leader key: [v] vulnerabilities, [p] pull requests, [l] last updated
		m.RepoLeaderMode = RepoLeaderSort
		return m, nil
	case "/":
		// '/' filter: filter repositories by name or label
		m.ActiveModal = ModalFilterRepoName
		m.RepoFilterInput.SetValue(m.RepoFilter)
		m.RepoFilterInput.Focus()
		return m, textinput.Blink
	case "#":
		// '#' filter: filter repositories by label
		m.ActiveModal = ModalFilterGroup
		m.ModalCursor = 0
		return m, nil
	case "v":
		// 'v' sort by vulnerabilities: toggle sorting from most to least vulnerabilities
		if m.RepoSortMode == RepoSortVulns || m.RepoSortVulnerabilities {
			m.RepoSortMode = RepoSortDefault
			m.RepoSortVulnerabilities = false
			m.RepoCursor = m.firstSelectableRepoIndex()
			return m, func() tea.Msg { return StatusToastMsg("Reset repository sort order to default") }
		}
		m.RepoSortMode = RepoSortVulns
		m.RepoSortVulnerabilities = true
		m.RepoCursor = m.firstSelectableRepoIndex()
		return m, func() tea.Msg { return StatusToastMsg("Sorted repositories by vulnerabilities (most to least)") }
	case "esc":
		// 'esc' clears active filters and sorting
		if m.RepoFilter != "" || m.RepoGroupFilter != "" || m.RepoSortMode != RepoSortDefault || m.RepoSortVulnerabilities || m.SquadFilterActive {
			m.RepoFilter = ""
			m.RepoGroupFilter = ""
			m.RepoSortMode = RepoSortDefault
			m.RepoSortVulnerabilities = false
			m.SquadFilterActive = false
			m.RepoCursor = m.firstSelectableRepoIndex()
			return m, func() tea.Msg { return StatusToastMsg("Cleared all repository filters and sorting") }
		}
		return m, nil
	}

	if len(items) == 0 || m.RepoCursor < 0 || m.RepoCursor >= len(items) {
		return m, nil
	}

	currentItem := items[m.RepoCursor]

	// 1. Key handling when cursor is on Tier Header
	if currentItem.Kind == ItemTierHeader {
		switch msg.String() {
		case "enter", " ":
			if m.CollapsedTiers == nil {
				m.CollapsedTiers = make(map[storage.RepoTier]bool)
			}
			isCollapsed := m.isTierCollapsed(currentItem.Tier)
			m.CollapsedTiers[currentItem.Tier] = !isCollapsed
			newItems := m.getRepoTableItems()
			if m.RepoCursor >= len(newItems) {
				m.RepoCursor = len(newItems) - 1
			}
			action := "Expanded"
			if !isCollapsed {
				action = "Collapsed"
			}
			return m, func() tea.Msg {
				return StatusToastMsg(fmt.Sprintf("%s %s tier", action, storage.TierName(currentItem.Tier)))
			}
		case "h", "left", "[":
			if m.CollapsedTiers == nil {
				m.CollapsedTiers = make(map[storage.RepoTier]bool)
			}
			m.CollapsedTiers[currentItem.Tier] = true
			newItems := m.getRepoTableItems()
			if m.RepoCursor >= len(newItems) {
				m.RepoCursor = len(newItems) - 1
			}
			return m, func() tea.Msg {
				return StatusToastMsg(fmt.Sprintf("Collapsed %s tier", storage.TierName(currentItem.Tier)))
			}
		case "l", "right", "]":
			if m.CollapsedTiers == nil {
				m.CollapsedTiers = make(map[storage.RepoTier]bool)
			}
			m.CollapsedTiers[currentItem.Tier] = false
			return m, func() tea.Msg {
				return StatusToastMsg(fmt.Sprintf("Expanded %s tier", storage.TierName(currentItem.Tier)))
			}
		case "E":
			// Expand all tiers
			if m.CollapsedTiers == nil {
				m.CollapsedTiers = make(map[storage.RepoTier]bool)
			}
			for _, t := range []storage.RepoTier{storage.TierHot, storage.TierWarm, storage.TierCold, storage.TierDead, storage.TierHidden} {
				m.CollapsedTiers[t] = false
			}
			return m, func() tea.Msg { return StatusToastMsg("Expanded all repository tiers") }
		case "C", "Z":
			// Collapse all tiers
			if m.CollapsedTiers == nil {
				m.CollapsedTiers = make(map[storage.RepoTier]bool)
			}
			for _, t := range []storage.RepoTier{storage.TierHot, storage.TierWarm, storage.TierCold, storage.TierDead, storage.TierHidden} {
				m.CollapsedTiers[t] = true
			}
			newItems := m.getRepoTableItems()
			if m.RepoCursor >= len(newItems) {
				m.RepoCursor = len(newItems) - 1
			}
			return m, func() tea.Msg { return StatusToastMsg("Collapsed all repository tiers") }
		case "s":
			return m, func() tea.Msg { return StatusToastMsg("Select a repository to open session") }
		case "r", "R":
			return m, func() tea.Msg { return StatusToastMsg("Select a repository to refresh") }
		default:
			return m, nil
		}
	}

	// 2. Key handling when cursor is on a child Pull Request row
	if currentItem.Kind == ItemRepoPR {
		pr := *currentItem.PR
		parentRepo := currentItem.Repo

		switch msg.String() {
		case "enter", "o":
			if pr.URL != "" {
				_ = OpenBrowser(pr.URL)
				return m, func() tea.Msg { return StatusToastMsg(fmt.Sprintf("Opened PR #%d in browser", pr.Number)) }
			}
			return m, nil
		case "y":
			if pr.URL != "" {
				_ = CopyToClipboard(pr.URL)
				return m, func() tea.Msg {
					return StatusToastMsg(fmt.Sprintf("Copied PR #%d URL to clipboard: %s", pr.Number, pr.URL))
				}
			}
			return m, nil
		case "m":
			// Merge approved pull request directly
			revStatus := strings.ToUpper(strings.TrimSpace(pr.ReviewDecision))
			if revStatus != "APPROVED" {
				if revStatus == "" {
					revStatus = "PENDING"
				}
				return m, func() tea.Msg {
					return StatusToastMsg(fmt.Sprintf("Cannot merge PR #%d: review status is %s (requires APPROVED)", pr.Number, revStatus))
				}
			}

			repoName := pr.Repo
			if repoName == "" && parentRepo != nil {
				repoName = parentRepo.Name
			}
			m = m.recordRepoInteraction(repoName)
			return m, m.mergePRCmd(repoName, pr.Number)
		case "h", "left", "[":
			// Jump cursor to parent repo row
			items := m.getRepoTableItems()
			for idx, it := range items {
				if it.Kind == ItemRepo && it.Repo != nil && parentRepo != nil && strings.EqualFold(it.Repo.Name, parentRepo.Name) {
					m.RepoCursor = idx
					break
				}
			}
			return m, nil
		case "C", "O":
			// Claim / toggle ownership of parent repo for active squad
			if parentRepo != nil {
				return m.toggleRepoOwnership(parentRepo.Name)
			}
			return m, nil
		case "t":
			// Request review / create PR task
			m.ActiveModal = ModalCreatePRTask
			m.PRTaskTarget = pr
			m.PRTaskPriority = github.PriorityMedium
			m.PRTaskFocusedField = 0
			m.PRTaskActiveButton = 0
			m.PRTaskMemberCursor = 0
			m.PRTaskCandidates = m.getSquadMembersForReview(pr.Author)
			m.PRTaskSelectedMembers = make(map[string]bool)
			return m, nil
		case "p":
			// Request Copilot review
			return m, func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				repoName := pr.Repo
				if repoName == "" && parentRepo != nil {
					repoName = parentRepo.Name
				}
				err := m.GHClient.TriggerCopilotReview(ctx, m.CurrentOrg, repoName, pr.Number)
				if err != nil {
					return StatusToastMsg(fmt.Sprintf("Failed to request Copilot review: %v", err))
				}
				return StatusToastMsg(fmt.Sprintf("Requested Copilot review for #%d in %s", pr.Number, repoName))
			}
		case "k", "c":
			// Checkout PR branch locally
			if parentRepo != nil {
				st := m.getGitStatus(parentRepo.Name)
				if !st.IsCloned {
					return m, func() tea.Msg { return StatusToastMsg("Repo " + parentRepo.Name + " is not cloned yet!") }
				}
				return m, func() tea.Msg {
					cmd := exec.Command("gh", "pr", "checkout", fmt.Sprintf("%d", pr.Number))
					cmd.Dir = st.LocalPath
					err := cmd.Run()
					if err != nil {
						return StatusToastMsg(fmt.Sprintf("Failed to checkout PR #%d: %v", pr.Number, err))
					}
					return StatusToastMsg(fmt.Sprintf("Checked out PR #%d in %s", pr.Number, st.LocalPath))
				}
			}
			return m, nil
		case "s":
			// Top-level [s]ession for parent repo
			if parentRepo != nil {
				st := m.getGitStatus(parentRepo.Name)
				if st.IsCloned {
					msgStr, err := GoToRepoSession(parentRepo.Name, st.LocalPath)
					if err != nil {
						return m, func() tea.Msg { return StatusToastMsg("Failed to switch session: " + err.Error()) }
					}
					return m, tea.Batch(
						func() tea.Msg { return StatusToastMsg(msgStr) },
						m.fetchSingleRepoCmd(m.CurrentOrg, parentRepo.Name),
					)
				}
				return m.startCloneWithAction(*parentRepo, RepoActionSession)
			}
			return m, nil
		case "r", "R":
			// Refresh parent repository state directly via direct query
			if parentRepo != nil {
				owner := parentRepo.Owner
				if owner == "" {
					owner = m.CurrentOrg
				}
				m.StatusMsg = fmt.Sprintf("Refreshing %s directly from GitHub...", parentRepo.Name)
				m.StatusMsgTimer = time.Now().Add(5 * time.Second)
				return m, tea.Batch(
					m.fetchSingleRepoCmd(owner, parentRepo.Name),
					m.scanWorkspacesCmd([]string{parentRepo.Name}),
					m.Spinner.Tick,
				)
			}
			return m, nil
		default:
			return m, nil
		}
	}

	// 3. Key handling when cursor is on a Repository row
	repo := *currentItem.Repo

	switch msg.String() {
	case "r", "R":
		// Refresh selected repository state directly via direct query
		owner := repo.Owner
		if owner == "" {
			owner = m.CurrentOrg
		}
		m.StatusMsg = fmt.Sprintf("Refreshing %s directly from GitHub...", repo.Name)
		m.StatusMsgTimer = time.Now().Add(5 * time.Second)
		return m, tea.Batch(
			m.fetchSingleRepoCmd(owner, repo.Name),
			m.scanWorkspacesCmd([]string{repo.Name}),
			m.Spinner.Tick,
		)
	case "s":
		// Top-level [s]ession command
		st := m.getGitStatus(repo.Name)
		if st.IsCloned {
			msgStr, err := GoToRepoSession(repo.Name, st.LocalPath)
			if err != nil {
				return m, func() tea.Msg { return StatusToastMsg("Failed to switch session: " + err.Error()) }
			}
			return m, tea.Batch(
				func() tea.Msg { return StatusToastMsg(msgStr) },
				m.fetchSingleRepoCmd(m.CurrentOrg, repo.Name),
			)
		}
		return m.startCloneWithAction(repo, RepoActionSession)
	case "o":
		m.RepoLeaderMode = RepoLeaderOpen
		return m, nil
	case "y":
		m.RepoLeaderMode = RepoLeaderYank
		return m, nil
	case "enter", " ":
		// Toggle PR expansion
		if currentItem.ChildCount > 0 {
			m.toggleRepoPRExpanded(&repo)
			newItems := m.getRepoTableItems()
			if m.RepoCursor >= len(newItems) {
				m.RepoCursor = len(newItems) - 1
			}
			return m, nil
		}
		return m, func() tea.Msg {
			return StatusToastMsg(fmt.Sprintf("No open pull requests for %s", repo.Name))
		}
	case "h":
		// Hide or unhide repository
		isCurrentlyHidden := m.isRepoHidden(repo.Name)
		newHidden := !isCurrentlyHidden
		if m.HiddenRepos == nil {
			m.HiddenRepos = make(map[string]bool)
		}
		if newHidden {
			m.HiddenRepos[strings.ToLower(repo.Name)] = true
			if len(m.Notifications) > 0 {
				var remainingNotifs []github.Notification
				for _, n := range m.Notifications {
					if !strings.EqualFold(n.Repository, repo.Name) &&
						!strings.EqualFold(n.Repository, repo.FullName) &&
						!m.isRepoHidden(n.Repository) {
						remainingNotifs = append(remainingNotifs, n)
					}
				}
				m.Notifications = remainingNotifs
			}
		} else {
			delete(m.HiddenRepos, strings.ToLower(repo.Name))
		}
		if m.DB != nil {
			_ = m.DB.SetRepoHidden(repo.Name, newHidden)
		}
		if m.Config != nil {
			_ = m.Config.SetRepoHidden(repo.Name, newHidden)
		}
		m = m.focusRepoCursor(repo.Name)
		actionDesc := "Hidden"
		if !newHidden {
			actionDesc = "Unhid"
		}
		return m, func() tea.Msg {
			return StatusToastMsg(fmt.Sprintf("%s repository '%s'", actionDesc, repo.Name))
		}
	case "left":
		// If PRs expanded, collapse them; otherwise jump cursor to parent tier header
		if m.isRepoPRExpanded(&repo) {
			m.toggleRepoPRExpanded(&repo)
			return m, nil
		}
		items := m.getRepoTableItems()
		for idx, it := range items {
			if it.Kind == ItemTierHeader && it.Tier == currentItem.Tier {
				m.RepoCursor = idx
				break
			}
		}
		return m, nil
	case "right":
		// If PRs not expanded and has PRs, expand them; if already expanded, step into first PR
		if !m.isRepoPRExpanded(&repo) && currentItem.ChildCount > 0 {
			m.toggleRepoPRExpanded(&repo)
			return m, nil
		} else if m.isRepoPRExpanded(&repo) && m.RepoCursor < len(items)-1 {
			m.RepoCursor++
			return m, nil
		}
		return m, nil
	case "[":
		// If PRs expanded, collapse PRs; otherwise collapse parent tier
		if m.isRepoPRExpanded(&repo) {
			m.toggleRepoPRExpanded(&repo)
			return m, nil
		}
		if m.CollapsedTiers == nil {
			m.CollapsedTiers = make(map[storage.RepoTier]bool)
		}
		m.CollapsedTiers[currentItem.Tier] = true
		newItems := m.getRepoTableItems()
		for idx, it := range newItems {
			if it.Kind == ItemTierHeader && it.Tier == currentItem.Tier {
				m.RepoCursor = idx
				break
			}
		}
		return m, nil
	case "]":
		// If PRs collapsed and has PRs, expand them
		if !m.isRepoPRExpanded(&repo) && currentItem.ChildCount > 0 {
			m.toggleRepoPRExpanded(&repo)
			return m, nil
		}
		return m, nil
	case "C", "O":
		// [C]laim replaces [O]
		return m.toggleRepoOwnership(repo.Name)
	case "c":
		// 'c' clone repo
		m = m.recordRepoInteraction(repo.Name)
		m = m.focusRepoCursor(repo.Name)
		st := m.getGitStatus(repo.Name)
		if st.IsCloned {
			return m, func() tea.Msg { return StatusToastMsg(repo.Name + " is already cloned!") }
		}
		return m.startCloneWithAction(repo, RepoActionNone)
	case "l", "g":
		// Open Labels modal for current repo
		m.ActiveModal = ModalManageGroups
		m.ModalCursor = 0
		m.SelectedRepoGroups = make(map[string]bool)
		for _, lbl := range m.getRepoLabels(&repo) {
			m.SelectedRepoGroups[lbl] = true
		}
		return m, nil
	case "E":
		// Expand all tiers
		if m.CollapsedTiers == nil {
			m.CollapsedTiers = make(map[storage.RepoTier]bool)
		}
		for _, t := range []storage.RepoTier{storage.TierHot, storage.TierWarm, storage.TierCold, storage.TierDead, storage.TierHidden} {
			m.CollapsedTiers[t] = false
		}
		return m, func() tea.Msg { return StatusToastMsg("Expanded all repository tiers") }
	case "Z":
		// Collapse all tiers
		if m.CollapsedTiers == nil {
			m.CollapsedTiers = make(map[storage.RepoTier]bool)
		}
		for _, t := range []storage.RepoTier{storage.TierHot, storage.TierWarm, storage.TierCold, storage.TierDead, storage.TierHidden} {
			m.CollapsedTiers[t] = true
		}
		newItems := m.getRepoTableItems()
		if m.RepoCursor >= len(newItems) {
			m.RepoCursor = len(newItems) - 1
		}
		return m, func() tea.Msg { return StatusToastMsg("Collapsed all repository tiers") }
	}
	return m, nil
}

func (m Model) startCloneWithAction(repo github.Repository, postAction RepoActionType) (tea.Model, tea.Cmd) {
	m = m.recordRepoInteraction(repo.Name)
	m = m.focusRepoCursor(repo.Name)
	var targetDir string
	if m.GitMgr != nil {
		targetDir = m.GitMgr.GetRepoPath(repo.Name)
	} else {
		targetDir = filepath.Join(os.Getenv("HOME"), "workspaces", repo.Name)
	}
	actionDesc := "session"
	if postAction == RepoActionEditor {
		actionDesc = "editor"
	} else if postAction == RepoActionShell {
		actionDesc = "shell"
	}

	if postAction != RepoActionNone {
		m.StatusMsg = fmt.Sprintf("Cloning %s to open %s...", repo.FullName, actionDesc)
	} else {
		m.StatusMsg = "Cloning " + repo.FullName + "..."
	}
	m.StatusMsgTimer = time.Now().Add(3 * time.Minute)

	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		err := github.CloneRepo(ctx, repo.FullName, targetDir)
		if err != nil {
			return RepoClonedMsg{RepoName: repo.Name, TargetDir: targetDir, Err: err, PostAction: postAction}
		}
		newStatus := git.InspectRepo(ctx, repo.Name, targetDir)
		return RepoClonedMsg{RepoName: repo.Name, TargetDir: targetDir, Status: newStatus, Err: nil, PostAction: postAction}
	}
}

func (m Model) renderReposView() string {
	repos := m.getDisplayRepos()
	items := m.getRepoTableItems(repos)

	if m.Loading && len(m.Repos) == 0 {
		return fmt.Sprintf("\n  %s Fetching repositories from GitHub...\n", m.Spinner.View())
	}
	if len(items) == 0 {
		if m.RepoFilter != "" || m.RepoGroupFilter != "" || m.SquadFilterActive {
			return "\n  No repositories match the current filter(s).\n  Press [Esc] to clear filters.\n"
		}
		return "\n  No repositories found for " + m.CurrentOrg + ".\n"
	}

	if m.RepoCursor >= len(items) {
		m.RepoCursor = len(items) - 1
	}
	if m.RepoCursor < 0 {
		m.RepoCursor = 0
	}

	cols := computeRepoColumns(m.Width)
	tmuxSessions := m.TmuxSessions
	if tmuxSessions == nil {
		tmuxSessions = GetTmuxSessions()
	}

	headerRow := lipgloss.JoinHorizontal(lipgloss.Top,
		cols.Cursor.Render(""),
		cols.Name.Render("REPOSITORY & LABELS"),
		cols.Summary.Render("(Vulns - Issues)"),
		cols.Git.Render("GIT STATUS"),
		cols.Path.Render("LOCAL PATH"),
	)
	renderedHeader := TableHeaderStyle.Render(headerRow)

	var rows []string

	// Active filter indicator if filtering or sorting is on
	var filterBadges []string
	if m.SquadFilterActive && m.Config != nil {
		squadName := m.Config.ActiveSquad
		if s := m.Config.GetActiveSquad(); s != nil && s.DisplayName != "" {
			squadName = s.DisplayName
		}
		filterBadges = append(filterBadges, lipgloss.NewStyle().Foreground(HighlightColor).Bold(true).Render("Squad: "+squadName))
	}
	if m.RepoGroupFilter != "" {
		filterBadges = append(filterBadges, lipgloss.NewStyle().Foreground(PrimaryColor).Bold(true).Render("Label: #"+strings.TrimPrefix(m.RepoGroupFilter, "#")))
	}
	if m.RepoFilter != "" {
		filterBadges = append(filterBadges, lipgloss.NewStyle().Foreground(SecondaryColor).Bold(true).Render(fmt.Sprintf("Name: %q", m.RepoFilter)))
	}
	effectiveSortMode := m.RepoSortMode
	if effectiveSortMode == RepoSortDefault && m.RepoSortVulnerabilities {
		effectiveSortMode = RepoSortVulns
	}
	switch effectiveSortMode {
	case RepoSortVulns:
		filterBadges = append(filterBadges, lipgloss.NewStyle().Foreground(WarningColor).Bold(true).Render("Sort: 🛡️ Most Vulns ↓"))
	case RepoSortPRs:
		filterBadges = append(filterBadges, lipgloss.NewStyle().Foreground(SecondaryColor).Bold(true).Render("Sort: ⑂ Open PRs ↓"))
	case RepoSortLastUpdated:
		filterBadges = append(filterBadges, lipgloss.NewStyle().Foreground(HighlightColor).Bold(true).Render("Sort: 🕒 Last Updated ↓"))
	}

	hotCount, warmCount, coldCount, deadCount, hiddenCount := m.getRepoTierCounts()
	tierSummary := fmt.Sprintf("🔥 %d Hot  🌞 %d Warm  🧊 %d Cold  💀 %d Dead", hotCount, warmCount, coldCount, deadCount)
	if hiddenCount > 0 {
		tierSummary += fmt.Sprintf("  🙈 %d Hidden", hiddenCount)
	}

	spinnerSuffix := ""
	if m.Loading {
		spinnerSuffix = fmt.Sprintf("  •  %s Syncing...", m.Spinner.View())
	}

	if len(filterBadges) > 0 {
		filterSummary := MetaBarStyle.Render("  🔍 ") + strings.Join(filterBadges, "  ") +
			MetaBarStyle.Render(fmt.Sprintf("  (%d of %d repos) • %s%s • [Esc] Clear", len(repos), len(m.Repos), tierSummary, spinnerSuffix))
		rows = append(rows, filterSummary)
	} else if len(m.Repos) > 0 {
		statusSuffix := "  (periodic polling focuses on Hot & Warm repos)"
		if m.EventSub != nil {
			statusSuffix = "  ⚡ Realtime Webhooks Active"
		}
		tierBar := MetaBarStyle.Render(fmt.Sprintf("  %s%s%s", tierSummary, spinnerSuffix, statusSuffix))
		rows = append(rows, tierBar)
	}

	var leaderPrompt string
	switch m.RepoLeaderMode {
	case RepoLeaderFilter:
		leaderPrompt = lipgloss.NewStyle().Background(lipgloss.Color("#2E3440")).Foreground(HighlightColor).Bold(true).
			Render("  🔍 FILTER: [n] Name  [l] Label  [s] Squad (toggle)  [Esc] Cancel")
	case RepoLeaderOpen:
		leaderPrompt = lipgloss.NewStyle().Background(lipgloss.Color("#2E3440")).Foreground(PrimaryColor).Bold(true).
			Render("  🌐 OPEN: [w/c] Web  [e] Editor  [s] Session  [p] PRs  [i] Issues  [a] Actions  [d] Security  [r] Releases  [n] Pulse  [Esc] Cancel")
	case RepoLeaderYank:
		leaderPrompt = lipgloss.NewStyle().Background(lipgloss.Color("#2E3440")).Foreground(WarningColor).Bold(true).
			Render("  📋 YANK: [w/c] Web URL  [s] Session  [p] PRs URL  [i] Issues URL  [a] Actions URL  [d] Security URL  [r] Releases URL  [n] Pulse URL  [Esc] Cancel")
	}
	if leaderPrompt != "" {
		rows = append(rows, leaderPrompt)
	}

	rows = append(rows, renderedHeader)

	visibleCount := m.Height - 9
	if visibleCount < 5 {
		visibleCount = 10
	}

	start := 0
	if m.RepoCursor >= visibleCount {
		start = m.RepoCursor - visibleCount + 1
	}
	end := start + visibleCount
	if end > len(items) {
		end = len(items)
	}

	totalW := m.Width - 4
	if totalW < 80 {
		totalW = 80
	}

	taskCountByRepo := make(map[string]int, len(m.PersonalTasks)+len(m.SquadTasks))
	for _, t := range m.PersonalTasks {
		taskCountByRepo[strings.ToLower(github.NormalizeRepoName(t.RepoName))]++
	}
	for _, t := range m.SquadTasks {
		taskCountByRepo[strings.ToLower(github.NormalizeRepoName(t.RepoName))]++
	}

	for i := start; i < end; i++ {
		item := items[i]

		if item.Kind == ItemTierHeader {
			arrow := "▼"
			if !item.Expanded {
				arrow = "▶"
			}
			tierIcon := storage.TierIcon(item.Tier)
			tierName := storage.TierName(item.Tier)

			unit := "repos"
			if item.ChildCount == 1 {
				unit = "repo"
			}
			headerText := fmt.Sprintf("%s %s %s (%d %s)", arrow, tierIcon, tierName, item.ChildCount, unit)
			if !item.Expanded {
				headerText += "  ── [Space to expand]"
			} else {
				headerText += "  ── [Space to collapse]"
			}

			// Add trailing rule line
			if len(headerText) < totalW {
				headerText += " " + strings.Repeat("─", totalW-len(headerText)-1)
			}

			var tierColor lipgloss.Color
			switch item.Tier {
			case storage.TierHot:
				tierColor = lipgloss.Color("#FF7B72")
			case storage.TierWarm:
				tierColor = lipgloss.Color("#E5C07B")
			case storage.TierCold:
				tierColor = lipgloss.Color("#79C0FF")
			case storage.TierDead:
				tierColor = lipgloss.Color("#6E7681")
			case storage.TierHidden:
				tierColor = lipgloss.Color("#555566")
			default:
				tierColor = lipgloss.Color("#E5C07B")
			}

			styledHeader := lipgloss.NewStyle().Foreground(tierColor).Bold(true).Render(headerText)

			cursorPrefix := "  "
			if i == m.RepoCursor {
				cursorPrefix = "▶ "
			}

			rowContent := cols.Cursor.Render(cursorPrefix) + styledHeader
			if i == m.RepoCursor {
				rows = append(rows, SelectedRowStyle.MaxHeight(1).Render(rowContent))
			} else {
				rows = append(rows, NormalRowStyle.MaxHeight(1).Render(rowContent))
			}
			continue
		}

		if item.Kind == ItemRepoPR {
			pr := *item.PR

			// Check if this is the last PR for the parent repo
			isLastPR := true
			if i+1 < len(items) && items[i+1].Kind == ItemRepoPR {
				isLastPR = false
			}
			treeSymbol := "├── "
			if isLastPR {
				treeSymbol = "└── "
			}

			prNumStr := fmt.Sprintf("#%-4d ", pr.Number)
			styledNum := lipgloss.NewStyle().Foreground(SecondaryColor).Bold(true).Render(prNumStr)
			cleanTitle := strings.ReplaceAll(strings.ReplaceAll(pr.Title, "\r", " "), "\n", " ")

			nameAvail := cols.NameWidth - len(treeSymbol) - len(prNumStr) - 6
			if nameAvail > 3 && len(cleanTitle) > nameAvail {
				cleanTitle = cleanTitle[:nameAvail-1] + "…"
			} else if nameAvail <= 3 {
				cleanTitle = ""
			}
			nameColText := "    " + lipgloss.NewStyle().Foreground(lipgloss.Color("#6E7681")).Render(treeSymbol) + styledNum + cleanTitle

			// Summary column: Review Decision + Comments
			var reviewBadge string
			switch pr.ReviewDecision {
			case "APPROVED":
				reviewBadge = ApprovedBadge.Render("APPROVED")
			case "CHANGES_REQUESTED":
				reviewBadge = ChangesReqBadge.Render("CHANGES_REQ")
			case "REVIEW_REQUIRED":
				reviewBadge = ReviewReqBadge.Render("REVIEW_REQ")
			default:
				reviewBadge = lipgloss.NewStyle().Foreground(MutedColor).Render("PENDING")
			}
			commentsBadge := fmt.Sprintf("💬 %d/%d", pr.UnresolvedComments, pr.TotalComments)
			if pr.UnresolvedComments > 0 {
				commentsBadge = lipgloss.NewStyle().Foreground(WarningColor).Render(commentsBadge)
			} else {
				commentsBadge = lipgloss.NewStyle().Foreground(MutedColor).Render(commentsBadge)
			}
			summaryColText := fmt.Sprintf("%s %s", reviewBadge, commentsBadge)

			// Git column: Author & Draft indicator
			authorText := "@" + pr.Author
			if pr.IsDraft {
				authorText += " (draft)"
			}
			if runewidth.StringWidth(authorText) > cols.GitWidth-2 && cols.GitWidth > 3 {
				authorText = runewidth.Truncate(authorText, cols.GitWidth-3, "…")
			}
			gitColText := lipgloss.NewStyle().Foreground(lipgloss.Color("#7D8590")).Render(authorText)

			// Path column: [Age] ⑂ Branch  Labels/URL
			var rawAge string
			if pr.CreatedAt.IsZero() {
				rawAge = "-"
			} else if pr.DaysStale() >= 3 {
				rawAge = fmt.Sprintf("%dd [!]", pr.DaysStale())
			} else {
				rawAge = formatTimeAgo(pr.CreatedAt)
			}
			ageText := fmt.Sprintf("[%s]", rawAge)
			var styledAge string
			if pr.DaysStale() >= 3 && !pr.CreatedAt.IsZero() {
				styledAge = StaleBadge.Render(ageText)
			} else {
				styledAge = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D8590")).Render(ageText)
			}

			var prDetails []string
			if pr.HeadRefName != "" {
				prDetails = append(prDetails, "⑂ "+pr.HeadRefName)
			}
			if len(pr.Labels) > 0 {
				prDetails = append(prDetails, strings.Join(pr.Labels, ", "))
			} else if pr.URL != "" {
				prDetails = append(prDetails, pr.URL)
			}
			extraText := strings.Join(prDetails, "  ")

			availExtra := cols.PathWidth - runewidth.StringWidth(ageText) - 1
			var pathColText string
			if availExtra > 0 && extraText != "" {
				if runewidth.StringWidth(extraText) > availExtra {
					extraText = runewidth.Truncate(extraText, availExtra, "…")
				}
				pathColText = styledAge + " " + lipgloss.NewStyle().Foreground(lipgloss.Color("#7D8590")).Render(extraText)
			} else {
				pathColText = styledAge
			}

			isFlashing, flashBg := m.isEntityFlashing(fmt.Sprintf("pr:%s#%d", pr.DisplayRepo(), pr.Number))
			if !isFlashing {
				isFlashing, flashBg = m.isEntityFlashing(fmt.Sprintf("pr:%s#%d", pr.Repo, pr.Number))
			}

			cursorPrefix := "  "
			if isFlashing {
				cursorPrefix = "⚡ "
			} else if i == m.RepoCursor {
				cursorPrefix = "▶ "
			}

			rowCells := lipgloss.JoinHorizontal(lipgloss.Top,
				cols.Cursor.Render(cursorPrefix),
				cols.Name.Render(nameColText),
				cols.Summary.Render(summaryColText),
				cols.Git.Render(gitColText),
				cols.Path.Render(pathColText),
			)

			if i == m.RepoCursor {
				if isFlashing {
					rows = append(rows, SelectedRowStyle.Background(flashBg).MaxHeight(1).Render(rowCells))
				} else {
					rows = append(rows, SelectedRowStyle.MaxHeight(1).Render(rowCells))
				}
			} else if isFlashing {
				rows = append(rows, lipgloss.NewStyle().Background(flashBg).MaxHeight(1).Render(rowCells))
			} else {
				rows = append(rows, NormalRowStyle.MaxHeight(1).Render(rowCells))
			}
			continue
		}

		// Repo row
		r := *item.Repo
		st := m.getGitStatus(r.Name)
		isDead := item.Tier == storage.TierDead || item.Tier == storage.TierHidden

		rNorm := strings.ToLower(github.NormalizeRepoName(r.Name))
		taskCount := taskCountByRepo[rNorm]

		// Name & Labels formatting
		repoDisplayName := r.Name
		if r.Owner != "" {
			repoDisplayName = r.Owner + "/" + r.Name
		} else if r.FullName != "" {
			repoDisplayName = r.FullName
		}
		labels := m.getRepoLabels(&r)

		var styledLabels []string
		for _, lbl := range labels {
			colorHex := m.getLabelColor(lbl)
			if isDead {
				styledLabels = append(styledLabels, lipgloss.NewStyle().Foreground(lipgloss.Color("#6E7681")).Render("#"+lbl))
			} else {
				styledLabels = append(styledLabels, lipgloss.NewStyle().Foreground(lipgloss.Color("#"+strings.TrimPrefix(colorHex, "#"))).Bold(true).Render("#"+lbl))
			}
		}

		// Squad ownership & Home Org indicators
		isOwned := false
		if m.Config != nil && m.Config.ActiveSquad != "" {
			isOwned = m.Config.IsRepoOwnedBySquad(m.Config.ActiveSquad, r.Name)
		}

		var styledName string
		prefix := ""
		if isOwned {
			prefix += "👥 "
		}
		if r.IsHomeOrg {
			prefix += "🏠 "
		}
		if isDead {
			styledName = lipgloss.NewStyle().Foreground(lipgloss.Color("#6E7681")).Render(prefix + repoDisplayName)
		} else if isOwned {
			styledName = lipgloss.NewStyle().Foreground(HighlightColor).Bold(true).Render(prefix + repoDisplayName)
		} else {
			styledName = lipgloss.NewStyle().Bold(true).Render(prefix + repoDisplayName)
		}

		rawNameLen := 2 + lipgloss.Width(prefix) + len(repoDisplayName)

		// Expansion badge for PRs if any open PRs
		if item.ChildCount > 0 {
			prUnit := "PRs"
			if item.ChildCount == 1 {
				prUnit = "PR"
			}
			var expBadge string
			if item.Expanded {
				expBadge = lipgloss.NewStyle().Foreground(HighlightColor).Bold(true).Render(fmt.Sprintf(" [▼ %d %s]", item.ChildCount, prUnit))
			} else {
				expBadge = lipgloss.NewStyle().Foreground(SecondaryColor).Bold(true).Render(fmt.Sprintf(" [▶ %d %s]", item.ChildCount, prUnit))
			}
			styledName += expBadge
			rawNameLen += 5 + len(fmt.Sprintf("%d", item.ChildCount)) + len(prUnit)
		}

		nameColText := "  " + styledName
		if len(styledLabels) > 0 {
			nameColText += " " + strings.Join(styledLabels, " ")
			for _, lbl := range labels {
				rawNameLen += 2 + len(lbl) // " #" + lbl
			}
		}

		// Display repository "About" text in italics with a slightly less light font
		about := strings.TrimSpace(r.Description)
		if about != "" && cols.NameWidth > rawNameLen+12 {
			avail := cols.NameWidth - rawNameLen - 4
			aboutSnippet := about
			if len(aboutSnippet) > avail {
				if avail > 4 {
					aboutSnippet = aboutSnippet[:avail-1] + "…"
				} else {
					aboutSnippet = ""
				}
			}
			if aboutSnippet != "" {
				var styledAbout string
				if isDead {
					styledAbout = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("#555566")).Render(aboutSnippet)
				} else {
					styledAbout = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("#7D8590")).Render(aboutSnippet)
				}
				nameColText += "  " + styledAbout
			}
		}

		// 3. (Vulns - Issues) column: ( clear ✓✓ ) / ( 01 06 05 ) - ( taskCount )
		var vulnText string
		sum := m.SecuritySummaries[r.Name]
		if isDead {
			deadStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6E7681")).Italic(true)
			if sum.Total == 0 {
				vulnText = deadStyle.Render("( clear ✓✓ )")
			} else {
				medRest := sum.Medium + sum.Low
				vulnText = deadStyle.Render(fmt.Sprintf("( %02d %02d %02d )", sum.Critical, sum.High, medRest))
			}
		} else {
			if sum.Total == 0 {
				vulnText = ClearBadgeStyle.Render("( clear ✓✓ )")
			} else {
				medRest := sum.Medium + sum.Low
				var cStr, hStr, mStr string
				if sum.Critical > 0 {
					cStr = lipgloss.NewStyle().Foreground(DangerColor).Bold(true).Render(fmt.Sprintf("%02d", sum.Critical))
				} else {
					cStr = lipgloss.NewStyle().Foreground(MutedColor).Render("00")
				}
				if sum.High > 0 {
					hStr = lipgloss.NewStyle().Foreground(WarningColor).Bold(true).Render(fmt.Sprintf("%02d", sum.High))
				} else {
					hStr = lipgloss.NewStyle().Foreground(MutedColor).Render("00")
				}
				if medRest > 0 {
					mStr = lipgloss.NewStyle().Foreground(lipgloss.Color("#E5C07B")).Bold(true).Render(fmt.Sprintf("%02d", medRest))
				} else {
					mStr = lipgloss.NewStyle().Foreground(MutedColor).Render("00")
				}
				vulnText = fmt.Sprintf("( %s %s %s )", cStr, hStr, mStr)
			}
		}

		var taskBadge string
		if isDead {
			taskBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("#6E7681")).Render(fmt.Sprintf("( %d )", taskCount))
		} else if taskCount > 0 {
			taskBadge = "( " + lipgloss.NewStyle().Foreground(lipgloss.Color("#98C379")).Bold(true).Render(fmt.Sprintf("%d", taskCount)) + " )"
		} else {
			taskBadge = lipgloss.NewStyle().Foreground(MutedColor).Render("( 0 )")
		}

		summaryColText := fmt.Sprintf("%s - %s", vulnText, taskBadge)

		// 4. Git badge
		gitW := cols.GitWidth
		if gitW > 4 {
			gitW = cols.GitWidth - 2
		}
		gitBadge := st.ShortBadge(gitW)
		var styledBadge string
		if isDead {
			styledBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("#6E7681")).Render(gitBadge)
		} else {
			switch st.State {
			case git.StateSynced:
				styledBadge = SyncedBadge.Render(gitBadge)
			case git.StateBehind:
				styledBadge = BehindBadge.Render(gitBadge)
			case git.StateAhead:
				styledBadge = lipgloss.NewStyle().Foreground(HighlightColor).Bold(true).Render(gitBadge)
			case git.StateDiverged:
				styledBadge = lipgloss.NewStyle().Foreground(WarningColor).Bold(true).Render(gitBadge)
			case git.StateDirty:
				styledBadge = DirtyBadge.Render(gitBadge)
			case git.StateNoUpstream:
				styledBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D8590")).Render(gitBadge)
			case git.StateNotCloned:
				styledBadge = NotClonedBadge.Render(gitBadge)
			default:
				styledBadge = gitBadge
			}
		}

		// 5. Local path (with [last pushed] prefix and optional (tmux: N) suffix)
		rawTime := formatTimeAgo(r.LastUpdated())
		timeBadgeText := fmt.Sprintf("[%s]", rawTime)
		if rawTime == "-" {
			timeBadgeText = "[-]"
		}

		var styledTime string
		if isDead {
			styledTime = lipgloss.NewStyle().Foreground(lipgloss.Color("#505565")).Render(timeBadgeText)
		} else {
			styledTime = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D8590")).Render(timeBadgeText)
		}
		timeW := runewidth.StringWidth(timeBadgeText)

		// Check for active tmux session
		var tmuxBadgeText string
		var styledTmux string
		var tmuxW int
		if sess, ok := FindTmuxSession(r.Name, tmuxSessions); ok && sess.Windows > 0 {
			tmuxBadgeText = fmt.Sprintf("(tmux: %d)", sess.Windows)
			tmuxW = runewidth.StringWidth(tmuxBadgeText)
			if isDead {
				styledTmux = lipgloss.NewStyle().Foreground(lipgloss.Color("#505565")).Render(tmuxBadgeText)
			} else {
				styledNum := lipgloss.NewStyle().Foreground(SecondaryColor).Bold(true).Render(fmt.Sprintf("%d", sess.Windows))
				styledTmux = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D8590")).Render("(tmux: ") + styledNum + lipgloss.NewStyle().Foreground(lipgloss.Color("#7D8590")).Render(")")
			}
		}

		rawPath := "-"
		if st.IsCloned {
			rawPath = config.CollapseHome(st.LocalPath)
		}

		// Calculate available width for path so (tmux: N) is guaranteed visible
		reserved := timeW
		if tmuxW > 0 {
			reserved += 1 + tmuxW
		}

		availForPath := cols.PathWidth - reserved - 1 // 1 for space after time
		truncatedPath := rawPath
		if availForPath <= 0 {
			truncatedPath = ""
		} else if runewidth.StringWidth(rawPath) > availForPath {
			truncatedPath = truncatePathLeft(rawPath, availForPath)
		}

		var styledPath string
		if isDead {
			styledPath = lipgloss.NewStyle().Foreground(lipgloss.Color("#6E7681")).Render(truncatedPath)
		} else {
			styledPath = lipgloss.NewStyle().Foreground(lipgloss.Color("#E0E0E0")).Render(truncatedPath)
		}

		var pathColText string
		if truncatedPath != "" {
			if tmuxW > 0 {
				pathColText = styledTime + " " + styledPath + " " + styledTmux
			} else {
				pathColText = styledTime + " " + styledPath
			}
		} else {
			if tmuxW > 0 {
				pathColText = styledTime + " " + styledTmux
			} else {
				pathColText = styledTime
			}
		}

		isFlashing, flashBg := m.isEntityFlashing("repo:" + r.FullName)
		if !isFlashing {
			isFlashing, flashBg = m.isEntityFlashing("repo:" + r.Name)
		}

		cursorPrefix := "  "
		if isFlashing {
			cursorPrefix = "⚡ "
		} else if i == m.RepoCursor {
			cursorPrefix = "▶ "
		}

		rowCells := lipgloss.JoinHorizontal(lipgloss.Top,
			cols.Cursor.Render(cursorPrefix),
			cols.Name.Render(nameColText),
			cols.Summary.Render(summaryColText),
			cols.Git.Render(styledBadge),
			cols.Path.Render(pathColText),
		)

		if i == m.RepoCursor {
			if isFlashing {
				rows = append(rows, SelectedRowStyle.Background(flashBg).MaxHeight(1).Render(rowCells))
			} else {
				rows = append(rows, SelectedRowStyle.MaxHeight(1).Render(rowCells))
			}
		} else if isFlashing {
			rows = append(rows, lipgloss.NewStyle().Background(flashBg).MaxHeight(1).Render(rowCells))
		} else {
			rows = append(rows, NormalRowStyle.MaxHeight(1).Render(rowCells))
		}
	}

	return strings.Join(rows, "\n")
}

func truncatePathLeft(p string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if runewidth.StringWidth(p) <= maxWidth {
		return p
	}
	if maxWidth == 1 {
		return "…"
	}
	runes := []rune(p)
	tailWidth := maxWidth - 1
	start := len(runes)
	curW := 0
	for start > 0 {
		rw := runewidth.RuneWidth(runes[start-1])
		if curW+rw > tailWidth {
			break
		}
		curW += rw
		start--
	}
	return "…" + string(runes[start:])
}

func formatTimeAgo(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	days := int(d.Hours() / 24)
	if days < 7 {
		return fmt.Sprintf("%dd", days)
	}
	if days < 30 {
		return fmt.Sprintf("%dw", days/7)
	}
	if days < 365 {
		return fmt.Sprintf("%dmo", days/30)
	}
	return fmt.Sprintf("%dy", days/365)
}
