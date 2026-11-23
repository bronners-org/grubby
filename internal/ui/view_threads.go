package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"

	"grubby/internal/github"
)

type ThreadFeedItemKind int

const (
	FeedItemTierHeader ThreadFeedItemKind = iota
	FeedItemThread
)

type ThreadFeedItem struct {
	Kind       ThreadFeedItemKind
	Tier       ThreadTier
	ChildCount int
	Expanded   bool
	Discussion *ScoredDiscussion
}

func (m Model) firstSelectableThreadIndex() int {
	items := m.getThreadFeedItems()
	for idx, it := range items {
		if it.Kind == FeedItemThread && it.Discussion != nil {
			return idx
		}
	}
	return 0
}

func (m Model) focusThreadCursor(id string) Model {
	items := m.getThreadFeedItems()
	for idx, it := range items {
		if it.Kind == FeedItemThread && it.Discussion != nil && it.Discussion.Discussion.ID == id {
			m.ThreadsCursor = idx
			break
		}
	}
	return m
}

func (m Model) getThreadFeedItems() []ThreadFeedItem {
	sorted := m.getSortedDiscussions()
	if len(sorted) == 0 {
		return nil
	}

	tierOrder := []ThreadTier{
		ThreadTierHot,
		ThreadTierWarm,
		ThreadTierCold,
		ThreadTierDead,
	}

	tierDiscussions := make(map[ThreadTier][]ScoredDiscussion)
	for _, sd := range sorted {
		tierDiscussions[sd.Tier] = append(tierDiscussions[sd.Tier], sd)
	}

	var items []ThreadFeedItem
	for _, tier := range tierOrder {
		list := tierDiscussions[tier]
		if len(list) == 0 {
			continue
		}
		collapsed := m.isThreadTierCollapsed(tier)
		items = append(items, ThreadFeedItem{
			Kind:       FeedItemTierHeader,
			Tier:       tier,
			ChildCount: len(list),
			Expanded:   !collapsed,
		})
		if !collapsed {
			for i := range list {
				sdCopy := list[i]
				items = append(items, ThreadFeedItem{
					Kind:       FeedItemThread,
					Tier:       tier,
					Discussion: &sdCopy,
				})
			}
		}
	}

	return items
}

func (m *Model) recalculateDiscussionOrder() {
	squad := ""
	var otherSquads []string
	if m.Config != nil {
		squad = m.Config.ActiveSquad
		for _, s := range m.Config.Squads {
			if !strings.EqualFold(s.Name, squad) {
				otherSquads = append(otherSquads, s.Name)
			}
		}
	}
	user := m.AuthUser
	filtered := github.FilterDiscussionsForSquad(m.Discussions, squad, otherSquads...)
	now := time.Now()
	var scored []ScoredDiscussion

	currentSelectedID := ""
	items := m.getThreadFeedItems()
	if len(items) > 0 && m.ThreadsCursor >= 0 && m.ThreadsCursor < len(items) {
		if items[m.ThreadsCursor].Kind == FeedItemThread && items[m.ThreadsCursor].Discussion != nil {
			currentSelectedID = items[m.ThreadsCursor].Discussion.Discussion.ID
		}
	}

	for _, d := range filtered {
		if cms, ok := m.DiscussionComments[d.ID]; ok && len(cms) > 0 {
			d.Comments = cms
			d.CommentsCount = len(cms)
		}
		sd := CalculateDiscussionScore(d, user, now)
		sd.IsUnread = m.isDiscussionUnread(d)
		scored = append(scored, sd)
	}
	m.SortedDiscussions = SortDiscussions(scored)
	m.LastThreadsSort = now

	if currentSelectedID != "" {
		newItems := m.getThreadFeedItems()
		for i, it := range newItems {
			if it.Kind == FeedItemThread && it.Discussion != nil && it.Discussion.Discussion.ID == currentSelectedID {
				m.ThreadsCursor = i
				break
			}
		}
	}
}

func (m Model) getSortedDiscussions() []ScoredDiscussion {
	if len(m.SortedDiscussions) > 0 {
		return m.SortedDiscussions
	}
	squad := ""
	var otherSquads []string
	if m.Config != nil {
		squad = m.Config.ActiveSquad
		for _, s := range m.Config.Squads {
			if !strings.EqualFold(s.Name, squad) {
				otherSquads = append(otherSquads, s.Name)
			}
		}
	}
	user := m.AuthUser

	filtered := github.FilterDiscussionsForSquad(m.Discussions, squad, otherSquads...)
	now := time.Now()
	var scored []ScoredDiscussion
	for _, d := range filtered {
		if cms, ok := m.DiscussionComments[d.ID]; ok && len(cms) > 0 {
			d.Comments = cms
			d.CommentsCount = len(cms)
		}
		sd := CalculateDiscussionScore(d, user, now)
		sd.IsUnread = m.isDiscussionUnread(d)
		scored = append(scored, sd)
	}
	return SortDiscussions(scored)
}

func (m Model) getVisibleDiscussions() []ScoredDiscussion {
	items := m.getThreadFeedItems()
	var visible []ScoredDiscussion
	for _, it := range items {
		if it.Kind == FeedItemThread && it.Discussion != nil {
			visible = append(visible, *it.Discussion)
		}
	}
	return visible
}

func (m Model) getSelectedDiscussion() *github.Discussion {
	items := m.getThreadFeedItems()
	if len(items) == 0 {
		return nil
	}
	idx := m.ThreadsCursor
	if idx < 0 {
		idx = 0
	}
	if idx >= len(items) {
		idx = len(items) - 1
	}
	item := items[idx]
	if item.Kind == FeedItemThread && item.Discussion != nil {
		disc := item.Discussion.Discussion
		return &disc
	}
	return nil
}

// wrapRunes replicates the exact word-wrapping algorithm used internally by
// bubbles/textarea to accurately predict line breaks and heights.
func wrapRunes(runes []rune, width int) [][]rune {
	if width <= 0 {
		width = 1
	}
	var (
		lines  = [][]rune{{}}
		word   = []rune{}
		row    int
		spaces int
	)

	repeatSpaces := func(n int) []rune {
		return []rune(strings.Repeat(" ", n))
	}

	for _, r := range runes {
		if unicode.IsSpace(r) {
			spaces++
		} else {
			word = append(word, r)
		}

		if spaces > 0 {
			if uniseg.StringWidth(string(lines[row]))+uniseg.StringWidth(string(word))+spaces > width {
				row++
				lines = append(lines, []rune{})
				lines[row] = append(lines[row], word...)
				lines[row] = append(lines[row], repeatSpaces(spaces)...)
				spaces = 0
				word = nil
			} else {
				lines[row] = append(lines[row], word...)
				lines[row] = append(lines[row], repeatSpaces(spaces)...)
				spaces = 0
				word = nil
			}
		} else {
			lastCharLen := runewidth.RuneWidth(word[len(word)-1])
			if uniseg.StringWidth(string(word))+lastCharLen > width {
				if len(lines[row]) > 0 {
					row++
					lines = append(lines, []rune{})
				}
				lines[row] = append(lines[row], word...)
				word = nil
			}
		}
	}

	if uniseg.StringWidth(string(lines[row]))+uniseg.StringWidth(string(word))+spaces >= width {
		lines = append(lines, []rune{})
		lines[row+1] = append(lines[row+1], word...)
		spaces++
		lines[row+1] = append(lines[row+1], repeatSpaces(spaces)...)
	} else {
		lines[row] = append(lines[row], word...)
		spaces++
		lines[row] = append(lines[row], repeatSpaces(spaces)...)
	}

	return lines
}

// calculateInputVisualLines computes how many terminal rows the textarea content
// will consume when soft-wrapped to the given width.
func calculateInputVisualLines(text string, width int) int {
	if text == "" {
		return 1
	}
	if width <= 0 {
		width = 70
	}
	paragraphs := strings.Split(text, "\n")
	totalLines := 0
	for _, para := range paragraphs {
		wLines := wrapRunes([]rune(para), width)
		totalLines += len(wLines)
	}
	if totalLines < 1 {
		totalLines = 1
	}
	return totalLines
}

// wrapFormattedText wraps formatted text (with ANSI escape sequences) into lines matching
// firstLineWidth for the first line and subsequentLineWidth for subsequent lines.
func wrapFormattedText(text string, firstLineWidth int, subsequentLineWidth int) []string {
	if text == "" {
		return []string{""}
	}
	if firstLineWidth <= 10 {
		firstLineWidth = 40
	}
	if subsequentLineWidth <= 10 {
		subsequentLineWidth = 40
	}

	var result []string
	paragraphs := strings.Split(text, "\n")
	for pIdx, para := range paragraphs {
		if strings.TrimSpace(para) == "" {
			if pIdx > 0 && len(result) > 0 {
				result = append(result, "")
			}
			continue
		}

		words := strings.Fields(para)
		if len(words) == 0 {
			if pIdx > 0 && len(result) > 0 {
				result = append(result, "")
			}
			continue
		}

		targetWidth := firstLineWidth
		if len(result) > 0 {
			targetWidth = subsequentLineWidth
		}

		var curLine strings.Builder
		curWidth := 0

		for _, word := range words {
			wWidth := lipgloss.Width(word)
			if curWidth == 0 {
				curLine.WriteString(word)
				curWidth = wWidth
			} else if curWidth+1+wWidth <= targetWidth {
				curLine.WriteRune(' ')
				curLine.WriteString(word)
				curWidth += 1 + wWidth
			} else {
				result = append(result, curLine.String())
				curLine.Reset()
				curLine.WriteString(word)
				curWidth = wWidth
				targetWidth = subsequentLineWidth
			}
		}
		if curLine.Len() > 0 {
			result = append(result, curLine.String())
		}
	}

	if len(result) == 0 {
		return []string{""}
	}
	return result
}

// setupReplyInputPrompt configures the reply textarea prompt, styling, and dynamic dimensions.
func (m *Model) setupReplyInputPrompt(width int) {
	author := m.AuthUser
	if author == "" {
		author = "user"
	}
	promptText := "@" + author + " "
	promptWidth := 4 + runewidth.StringWidth(promptText)
	styledPrompt := "    " + lipgloss.NewStyle().Foreground(SecondaryColor).Bold(true).Render(promptText)
	indentSpaces := strings.Repeat(" ", promptWidth)

	m.ThreadsReplyInput.Prompt = ""
	m.ThreadsReplyInput.SetPromptFunc(promptWidth, func(lineIdx int) string {
		if lineIdx == 0 {
			return styledPrompt
		}
		return indentSpaces
	})
	m.ThreadsReplyInput.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(SecondaryColor).Bold(true)
	m.ThreadsReplyInput.FocusedStyle.Text = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF"))
	m.ThreadsReplyInput.Placeholder = "Write a reply... (Enter to send, Shift+Enter for newline, Esc to cancel)"
	m.ThreadsReplyInput.ShowLineNumbers = false
	m.ThreadsReplyInput.CharLimit = 2000

	w := width - 6
	if w < 76 {
		w = 76
	}
	m.ThreadsReplyInput.SetWidth(w)

	lines := calculateInputVisualLines(m.ThreadsReplyInput.Value(), m.ThreadsReplyInput.Width())
	if lines < 1 {
		lines = 1
	}
	if lines > 12 {
		lines = 12
	}
	m.ThreadsReplyInput.SetHeight(lines)
	m.ensureComposerScrolledToFit()
}

// ensureComposerScrolledToFit checks if line 0 (containing author prompt) has scrolled
// out of view when all visual lines can fit within the textarea height.
// If so, it resets the internal viewport YOffset to 0.
func (m *Model) ensureComposerScrolledToFit() {
	author := m.AuthUser
	if author == "" {
		author = "user"
	}
	lines := calculateInputVisualLines(m.ThreadsReplyInput.Value(), m.ThreadsReplyInput.Width())
	if lines <= m.ThreadsReplyInput.Height() {
		if !strings.Contains(m.ThreadsReplyInput.View(), "@"+author) {
			m.ThreadsReplyInput, _ = m.ThreadsReplyInput.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'<'}, Alt: true})
			m.ThreadsReplyInput, _ = m.ThreadsReplyInput.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'>'}, Alt: true})
		}
	}
}

// setupEditInputPrompt configures the edit textarea prompt, styling, and dynamic dimensions.
func (m *Model) setupEditInputPrompt(width int) {
	author := m.ThreadEditMessageAuthor
	if author == "" {
		author = m.AuthUser
	}
	if author == "" {
		author = "user"
	}

	indent := "  "
	if !m.ThreadEditIsOP {
		indent = "    "
	}

	promptText := "@" + author + " "
	promptWidth := runewidth.StringWidth(indent) + runewidth.StringWidth(promptText)
	styledPrompt := indent + lipgloss.NewStyle().Foreground(SecondaryColor).Bold(true).Render(promptText)
	indentSpaces := strings.Repeat(" ", promptWidth)

	m.ThreadEditMessageInput.Prompt = ""
	m.ThreadEditMessageInput.SetPromptFunc(promptWidth, func(lineIdx int) string {
		if lineIdx == 0 {
			return styledPrompt
		}
		return indentSpaces
	})
	m.ThreadEditMessageInput.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(SecondaryColor).Bold(true)
	m.ThreadEditMessageInput.FocusedStyle.Text = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF"))
	m.ThreadEditMessageInput.Placeholder = "Edit message... (Enter to save, Shift+Enter for newline, Esc to cancel)"
	m.ThreadEditMessageInput.ShowLineNumbers = false
	m.ThreadEditMessageInput.CharLimit = 10000

	w := width - 6
	if w < 76 {
		w = 76
	}
	m.ThreadEditMessageInput.SetWidth(w)

	lines := calculateInputVisualLines(m.ThreadEditMessageInput.Value(), m.ThreadEditMessageInput.Width())
	if lines < 1 {
		lines = 1
	}
	if lines > 20 {
		lines = 20
	}
	m.ThreadEditMessageInput.SetHeight(lines)
	m.ensureEditComposerScrolledToFit()
}

// ensureEditComposerScrolledToFit checks if line 0 (containing author prompt) has scrolled
// out of view when all visual lines can fit within the textarea height.
// If so, it resets the internal viewport YOffset to 0.
func (m *Model) ensureEditComposerScrolledToFit() {
	author := m.ThreadEditMessageAuthor
	if author == "" {
		author = m.AuthUser
	}
	if author == "" {
		author = "user"
	}
	lines := calculateInputVisualLines(m.ThreadEditMessageInput.Value(), m.ThreadEditMessageInput.Width())
	if lines <= m.ThreadEditMessageInput.Height() {
		if !strings.Contains(m.ThreadEditMessageInput.View(), "@"+author) {
			m.ThreadEditMessageInput, _ = m.ThreadEditMessageInput.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'<'}, Alt: true})
			m.ThreadEditMessageInput, _ = m.ThreadEditMessageInput.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'>'}, Alt: true})
		}
	}
}

// startEditMessage sets up the inline edit prompt for the given message.
func (m *Model) startEditMessage(discID, targetID, targetAuthor, targetBody string, isOP bool) {
	m.ThreadsEditActive = true
	m.ThreadsReplyActive = false
	m.ActiveModal = ModalNone
	m.ThreadEditMessageID = targetID
	m.ThreadEditDiscID = discID
	m.ThreadEditIsOP = isOP
	m.ThreadEditMessageAuthor = targetAuthor
	if !isOP {
		if m.ThreadsCollapsedIDs != nil {
			delete(m.ThreadsCollapsedIDs, discID)
		}
		m.ThreadsExpandedID = discID
	}

	m.ThreadEditMessageInput = textarea.New()
	m.ThreadEditMessageInput.SetValue(targetBody)
	m.setupEditInputPrompt(m.Width)
	m.ThreadEditMessageInput.Focus()
	m.ThreadEditMessageInput.CursorEnd()
	m.AutocompleteActive = false
}

// startReplyToThread sets up the inline reply prompt for the given discussion.
func (m *Model) startReplyToThread(disc *github.Discussion) {
	if disc == nil {
		return
	}
	m.ThreadsEditActive = false
	m.ThreadEditMessageID = ""
	if m.ThreadsCollapsedIDs != nil {
		delete(m.ThreadsCollapsedIDs, disc.ID)
	}
	m.ThreadsReplyDiscussionID = disc.ID
	m.ThreadsExpandedID = disc.ID
	m.ThreadsReplyActive = true

	m.setupReplyInputPrompt(m.Width)
	m.ThreadsReplyInput.Reset()
	m.ThreadsReplyInput.SetHeight(1)
	m.ThreadsReplyInput.Focus()
	m.AutocompleteActive = false

	if m.DB != nil {
		_ = m.DB.MarkDiscussionRead(disc.ID)
		for i := range m.Discussions {
			if m.Discussions[i].ID == disc.ID {
				m.Discussions[i].LastReadAt = time.Now()
				break
			}
		}
	}
}

// detectAutocomplete scans backwards from the end of val to detect '@' or '#' triggers.
func detectAutocomplete(val string) (bool, rune, string) {
	if len(val) == 0 {
		return false, 0, ""
	}
	lastIdx := len(val) - 1
	startIdx := lastIdx
	for startIdx > 0 {
		ch := rune(val[startIdx-1])
		if ch == ' ' || ch == '\n' || ch == '\t' || ch == '(' || ch == '[' {
			break
		}
		startIdx--
	}
	word := val[startIdx : lastIdx+1]
	if len(word) >= 1 {
		if word[0] == '@' {
			return true, '@', word[1:]
		}
		if word[0] == '#' {
			return true, '#', word[1:]
		}
	}
	return false, 0, ""
}

// insertAutocomplete replaces the active trigger word at the end of val with the selected key.
func insertAutocomplete(val string, selectedKey string) string {
	if len(val) == 0 {
		return selectedKey + " "
	}
	lastIdx := len(val) - 1
	startIdx := lastIdx
	for startIdx > 0 {
		ch := rune(val[startIdx-1])
		if ch == ' ' || ch == '\n' || ch == '\t' || ch == '(' || ch == '[' {
			break
		}
		startIdx--
	}
	return val[:startIdx] + selectedKey + " "
}

type hashCandidate struct {
	item      AutocompleteItem
	repoName  string
	prNumber  int
	updatedAt time.Time
	score     int
}

func scoreHashItem(it AutocompleteItem, repoName string, prNumber int, q string) int {
	if q == "" {
		if strings.HasPrefix(it.Description, "PR in") || it.Description == "Pull Request" {
			return 20
		}
		return 10
	}

	keyLower := strings.ToLower(it.Key)
	keyTrimmed := strings.TrimPrefix(keyLower, "#")
	titleLower := strings.ToLower(it.Title)
	repoLower := strings.ToLower(repoName)
	prNumStr := strconv.Itoa(prNumber)

	// 1. Exact matches
	if q == keyTrimmed || q == keyLower {
		return 100
	}
	if prNumber > 0 && q == prNumStr {
		return 95
	}
	if repoLower != "" && q == repoLower {
		if it.Description == "Repository" {
			return 90
		}
		return 85
	}
	if prNumber > 0 && repoLower != "" {
		if q == fmt.Sprintf("%s#%d", repoLower, prNumber) ||
			q == fmt.Sprintf("%s/%d", repoLower, prNumber) ||
			q == fmt.Sprintf("%s %d", repoLower, prNumber) {
			return 95
		}
	}

	// 2. Prefix matches
	if strings.HasPrefix(keyTrimmed, q) {
		return 80
	}
	if repoLower != "" && strings.HasPrefix(repoLower, q) {
		if it.Description == "Repository" {
			return 75
		}
		return 70
	}
	if prNumber > 0 && strings.HasPrefix(prNumStr, q) {
		return 70
	}
	if strings.HasPrefix(titleLower, q) {
		return 65
	}

	// 3. Substring matches
	if strings.Contains(keyLower, q) {
		return 50
	}
	if repoLower != "" && strings.Contains(repoLower, q) {
		return 45
	}
	if strings.Contains(titleLower, q) {
		return 40
	}
	if strings.Contains(strings.ToLower(it.Description), q) {
		return 20
	}

	return 0
}

func (m Model) getHashAutocompleteSuggestions(query string) []AutocompleteItem {
	q := strings.TrimPrefix(strings.TrimSpace(strings.ToLower(query)), "#")
	seenKeys := make(map[string]bool)
	var candidates []hashCandidate

	// 1. Index open PRs in non-hidden repositories
	for _, pr := range m.PRs {
		if strings.EqualFold(pr.State, "CLOSED") || strings.EqualFold(pr.State, "MERGED") {
			continue
		}
		repo := pr.Repo
		if repo == "" {
			repo = pr.DisplayRepo()
		}
		if m.isRepoHidden(repo) || (pr.RepoFullName != "" && m.isRepoHidden(pr.RepoFullName)) {
			continue
		}
		repoShort := repo
		if idx := strings.LastIndex(repoShort, "/"); idx != -1 && idx < len(repoShort)-1 {
			repoShort = repoShort[idx+1:]
		}

		key := fmt.Sprintf("#%s#%d", repoShort, pr.Number)
		seenKey := fmt.Sprintf("%s#%d", strings.ToLower(repoShort), pr.Number)
		if seenKeys[seenKey] {
			continue
		}
		seenKeys[seenKey] = true

		url := pr.URL
		if url == "" && m.CurrentOrg != "" {
			url = fmt.Sprintf("https://github.com/%s/%s/pull/%d", m.CurrentOrg, repoShort, pr.Number)
		}

		item := AutocompleteItem{
			Key:         key,
			Title:       pr.Title,
			Description: fmt.Sprintf("PR in %s", repoShort),
			InsertText:  fmt.Sprintf("[%s#%d](%s)", repoShort, pr.Number, url),
			URL:         url,
		}
		candidates = append(candidates, hashCandidate{
			item:      item,
			repoName:  repoShort,
			prNumber:  pr.Number,
			updatedAt: pr.UpdatedAt,
		})
	}

	// 2. Index non-hidden repositories
	for _, r := range m.Repos {
		if m.isRepoHidden(r.Name) || (r.FullName != "" && m.isRepoHidden(r.FullName)) {
			continue
		}
		key := "#" + r.Name
		if seenKeys[strings.ToLower(key)] {
			continue
		}
		seenKeys[strings.ToLower(key)] = true

		url := fmt.Sprintf("https://github.com/%s", r.FullName)
		if r.FullName == "" && m.CurrentOrg != "" {
			url = fmt.Sprintf("https://github.com/%s/%s", m.CurrentOrg, r.Name)
		}
		item := AutocompleteItem{
			Key:         key,
			Title:       r.FullName,
			Description: "Repository",
			InsertText:  fmt.Sprintf("[#%s](%s)", r.Name, url),
			URL:         url,
		}
		candidates = append(candidates, hashCandidate{
			item:      item,
			repoName:  r.Name,
			updatedAt: r.LastUpdated(),
		})
	}

	// 3. Index open squad and personal tasks
	for _, tk := range m.SquadTasks {
		if strings.EqualFold(tk.State, "closed") {
			continue
		}
		key := fmt.Sprintf("#%d", tk.ID)
		if seenKeys[key] {
			continue
		}
		seenKeys[key] = true
		url := tk.URL
		if url == "" && tk.RepoOwner != "" && tk.RepoName != "" {
			url = fmt.Sprintf("https://github.com/%s/%s/issues/%d", tk.RepoOwner, tk.RepoName, tk.Number)
		}
		label := fmt.Sprintf("#%d", tk.Number)
		if tk.Number == 0 {
			label = fmt.Sprintf("Task #%d", tk.ID)
		}
		item := AutocompleteItem{
			Key:         key,
			Title:       tk.Title,
			Description: "Squad Task",
			InsertText:  fmt.Sprintf("[%s](%s)", label, url),
			URL:         url,
		}
		candidates = append(candidates, hashCandidate{
			item:      item,
			prNumber:  tk.Number,
			updatedAt: tk.UpdatedAt,
		})
	}
	for _, tk := range m.PersonalTasks {
		if strings.EqualFold(tk.State, "closed") {
			continue
		}
		key := fmt.Sprintf("#%d", tk.ID)
		if seenKeys[key] {
			continue
		}
		seenKeys[key] = true
		url := tk.URL
		if url == "" && tk.RepoOwner != "" && tk.RepoName != "" {
			url = fmt.Sprintf("https://github.com/%s/%s/issues/%d", tk.RepoOwner, tk.RepoName, tk.Number)
		}
		label := fmt.Sprintf("#%d", tk.Number)
		if tk.Number == 0 {
			label = fmt.Sprintf("Task #%d", tk.ID)
		}
		item := AutocompleteItem{
			Key:         key,
			Title:       tk.Title,
			Description: "Personal Task",
			InsertText:  fmt.Sprintf("[%s](%s)", label, url),
			URL:         url,
		}
		candidates = append(candidates, hashCandidate{
			item:      item,
			prNumber:  tk.Number,
			updatedAt: tk.UpdatedAt,
		})
	}

	// 4. Score and filter candidates
	var matched []hashCandidate
	for _, c := range candidates {
		score := scoreHashItem(c.item, c.repoName, c.prNumber, q)
		if score > 0 {
			c.score = score
			matched = append(matched, c)
		}
	}

	// Sort by score desc, then by updatedAt desc
	for i := 0; i < len(matched)-1; i++ {
		for j := i + 1; j < len(matched); j++ {
			if matched[i].score < matched[j].score ||
				(matched[i].score == matched[j].score && matched[i].updatedAt.Before(matched[j].updatedAt)) {
				matched[i], matched[j] = matched[j], matched[i]
			}
		}
	}

	var results []AutocompleteItem
	for _, m := range matched {
		results = append(results, m.item)
		if len(results) >= 8 {
			break
		}
	}
	return results
}

// getAutocompleteSuggestions returns suggestions matching trigger and query.
func (m Model) getAutocompleteSuggestions(trigger rune, query string) []AutocompleteItem {
	if trigger == '#' {
		return m.getHashAutocompleteSuggestions(query)
	}

	var suggestions []AutocompleteItem
	queryLower := strings.ToLower(query)
	seen := make(map[string]bool)

	if trigger == '@' {
		// 1. Squad members from active squad
		if m.Config != nil && m.Config.ActiveSquad != "" {
			for _, sq := range m.Config.Squads {
				if strings.EqualFold(sq.Name, m.Config.ActiveSquad) {
					for _, mem := range sq.Members {
						key := "@" + mem
						if !seen[key] {
							seen[key] = true
							url := fmt.Sprintf("https://github.com/%s", mem)
							suggestions = append(suggestions, AutocompleteItem{
								Key:         key,
								Title:       mem,
								Description: "Squad Member",
								InsertText:  fmt.Sprintf("[@%s](%s)", mem, url),
								URL:         url,
							})
						}
					}
					break
				}
			}
		}

		// 2. Thread participants
		for _, d := range m.Discussions {
			if d.Author != "" {
				key := "@" + d.Author
				if !seen[key] {
					seen[key] = true
					url := fmt.Sprintf("https://github.com/%s", d.Author)
					suggestions = append(suggestions, AutocompleteItem{
						Key:         key,
						Title:       d.Author,
						Description: "Thread Author",
						InsertText:  fmt.Sprintf("[@%s](%s)", d.Author, url),
						URL:         url,
					})
				}
			}
		}

		// 3. Cached org members
		for login, mem := range m.ProfileCache {
			key := "@" + login
			if !seen[key] {
				seen[key] = true
				name := mem.Name
				if name == "" {
					name = login
				}
				url := fmt.Sprintf("https://github.com/%s", login)
				suggestions = append(suggestions, AutocompleteItem{
					Key:         key,
					Title:       name,
					Description: "Member",
					InsertText:  fmt.Sprintf("[@%s](%s)", login, url),
					URL:         url,
				})
			}
		}
	}

	// Filter by query
	var filtered []AutocompleteItem
	for _, it := range suggestions {
		if queryLower == "" || strings.Contains(strings.ToLower(it.Key), queryLower) || strings.Contains(strings.ToLower(it.Title), queryLower) {
			filtered = append(filtered, it)
			if len(filtered) >= 8 {
				break
			}
		}
	}
	return filtered
}

func renderAutocompletePopup(items []AutocompleteItem, cursor int, width int) string {
	if len(items) == 0 {
		return ""
	}
	var rows []string
	header := lipgloss.NewStyle().Foreground(SecondaryColor).Bold(true).Render(" Suggestions ([Tab/Enter] to insert, [Esc] to dismiss):")
	rows = append(rows, header)
	for i, item := range items {
		prefix := "  "
		style := lipgloss.NewStyle().Foreground(lipgloss.Color("#E0E0E0"))
		if i == cursor {
			prefix = lipgloss.NewStyle().Foreground(SecondaryColor).Bold(true).Render("▶ ")
			style = lipgloss.NewStyle().Foreground(SecondaryColor).Bold(true)
		}
		keyStyled := style.Render(fmt.Sprintf("%-18s", item.Key))
		titleStyled := lipgloss.NewStyle().Foreground(lipgloss.Color("#A0A0A0")).Render(runewidth.Truncate(item.Title, 30, "…"))
		descStyled := lipgloss.NewStyle().Foreground(MutedColor).Italic(true).Render(fmt.Sprintf("(%s)", item.Description))
		rows = append(rows, fmt.Sprintf("%s%s %s %s", prefix, keyStyled, titleStyled, descStyled))
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(SecondaryColor).
		Background(lipgloss.Color("#1E2030")).
		Padding(0, 1).
		Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}

func (m Model) renderThreadsView() string {
	width := m.Width
	if width < 80 {
		width = 80
	}
	feedWidth := width - 4
	if feedWidth < 76 {
		feedWidth = 76
	}

	sorted := m.getSortedDiscussions()
	hotCount := 0
	warmCount := 0
	coldCount := 0
	deadCount := 0
	for _, sd := range sorted {
		switch sd.Tier {
		case ThreadTierHot:
			hotCount++
		case ThreadTierWarm:
			warmCount++
		case ThreadTierCold:
			coldCount++
		case ThreadTierDead:
			deadCount++
		}
	}

	items := m.getThreadFeedItems()
	isCursorOnTierHeader := false
	if len(items) > 0 && m.ThreadsCursor >= 0 && m.ThreadsCursor < len(items) {
		if items[m.ThreadsCursor].Kind == FeedItemTierHeader {
			isCursorOnTierHeader = true
		}
	}

	// 1. Top Meta Banner (removed "Squad: [my-squad] |", replicated repo UI: 🔥 Hot 🌞 Warm 🧊 Cold 💀 Dead)
	var bannerText string
	if m.ThreadsEditActive {
		targetType := "Reply"
		if m.ThreadEditIsOP {
			targetType = "OP"
		}
		bannerText = fmt.Sprintf("  [EDITING %s MESSAGE by @%s]  |  [Enter/Ctrl+S] Save  [Shift+Enter] Line Break  [@] Squad  [#] Refs  [Esc] Cancel", targetType, m.ThreadEditMessageAuthor)
	} else if m.ThreadsFocused {
		disc := m.getSelectedDiscussion()
		discNum := 0
		if disc != nil {
			discNum = disc.Number
		}
		bannerText = fmt.Sprintf("  [FOCUSED THREAD #%d]  |  [f/Esc] Exit Focus  [r] Reply  [e] Edit  [t] Title  [→/l] Messages  [y] Yank", discNum)
	} else if isCursorOnTierHeader {
		bannerText = "  [TIER HEADER]  |  [Space/Enter] Toggle Tier  [←/h] Collapse  [→/l] Expand  [Z] Collapse All  [E] Expand All  [j/k] Navigate"
	} else {
		tierSummary := fmt.Sprintf("🔥 %d Hot  🌞 %d Warm  🧊 %d Cold  💀 %d Dead", hotCount, warmCount, coldCount, deadCount)
		bannerText = fmt.Sprintf("  %s  |  [c] New  [r] Reply  [e] Edit  [t] Title  [→] Expand  [←] Collapse  [f] Focus  [y] Yank  [R] Refresh", tierSummary)
	}
	banner := MetaBarStyle.Width(width).Render(bannerText)

	var feedBlocks []string

	if len(items) == 0 {
		feedBlocks = append(feedBlocks, lipgloss.NewStyle().Foreground(MutedColor).Render("\n  No discussions in this squad.\n  Press [c] to start a new thread!"))
	} else {
		authorStyle := lipgloss.NewStyle().Foreground(PrimaryColor).Bold(true)
		dotStyle := lipgloss.NewStyle().Foreground(MutedColor)
		dot := dotStyle.Render("•")
		timeStyle := lipgloss.NewStyle().Foreground(MutedColor)
		titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#708090"))

		displayItems := items
		if m.ThreadsFocused {
			disc := m.getSelectedDiscussion()
			if disc != nil {
				for _, it := range items {
					if it.Kind == FeedItemThread && it.Discussion != nil && it.Discussion.Discussion.ID == disc.ID {
						displayItems = []ThreadFeedItem{it}
						break
					}
				}
			}
		}

		for idx, it := range displayItems {
			if it.Kind == FeedItemTierHeader {
				arrow := "▼"
				if !it.Expanded {
					arrow = "▶"
				}
				tierIcon := it.Tier.Icon()
				tierName := strings.ToUpper(it.Tier.String())

				unit := "threads"
				if it.ChildCount == 1 {
					unit = "thread"
				}
				headerText := fmt.Sprintf("%s %s %s (%d %s)", arrow, tierIcon, tierName, it.ChildCount, unit)
				if !it.Expanded {
					headerText += "  ── [Space to expand]"
				} else {
					headerText += "  ── [Space to collapse]"
				}

				totalW := feedWidth
				if len(headerText) < totalW {
					headerText += " " + strings.Repeat("─", totalW-len(headerText)-1)
				}

				var tierColor lipgloss.Color
				switch it.Tier {
				case ThreadTierHot:
					tierColor = lipgloss.Color("#FF7B72")
				case ThreadTierWarm:
					tierColor = lipgloss.Color("#E5C07B")
				case ThreadTierCold:
					tierColor = lipgloss.Color("#79C0FF")
				case ThreadTierDead:
					tierColor = lipgloss.Color("#6E7681")
				default:
					tierColor = lipgloss.Color("#E5C07B")
				}

				styledHeader := lipgloss.NewStyle().Foreground(tierColor).Bold(true).Render(headerText)

				cursorPrefix := "  "
				if idx == m.ThreadsCursor && !m.ThreadsFocused {
					cursorPrefix = lipgloss.NewStyle().Foreground(SecondaryColor).Bold(true).Render("▶ ")
				}

				feedBlocks = append(feedBlocks, cursorPrefix+styledHeader)
				continue
			}

			// it.Kind == FeedItemThread:
			sd := *it.Discussion
			disc := sd.Discussion
			isCurrentThread := (idx == m.ThreadsCursor || m.ThreadsFocused)
			hasUnread := sd.IsUnread || m.isDiscussionUnread(disc)
			isExpanded := m.isThreadExpanded(disc.ID, hasUnread)
			isMsgLevel := isCurrentThread && (m.ThreadsSelectedMsgIdx >= 0)

			// Thread Title rendered in light grey above OP
			titleText := disc.Title
			if titleText == "" {
				titleText = fmt.Sprintf("Thread #%d", disc.Number)
			}
			feedBlocks = append(feedBlocks, "  "+titleStyle.Render("🧵 "+titleText))

			// Cursor marker
			cursorPrefix := "  "
			if isCurrentThread {
				if !isMsgLevel || m.ThreadsSelectedMsgIdx == 0 {
					cursorPrefix = lipgloss.NewStyle().Foreground(SecondaryColor).Bold(true).Render("▶ ")
				}
			}

			flashing, _ := m.isEntityFlashing("discussion:" + disc.ID)
			flashSymbol := ""
			if flashing {
				flashSymbol = lipgloss.NewStyle().Foreground(WarningColor).Bold(true).Render("⚡ ")
			}

			// OP message formatting: mark unread messages with ● (green)
			if m.ThreadsEditActive && m.ThreadEditMessageID == disc.ID {
				m.setupEditInputPrompt(width)
				m.ensureEditComposerScrolledToFit()
				editLine := m.ThreadEditMessageInput.View()
				feedBlocks = append(feedBlocks, editLine)
				if m.AutocompleteActive && len(m.AutocompleteItems) > 0 {
					feedBlocks = append(feedBlocks, "  "+renderAutocompletePopup(m.AutocompleteItems, m.AutocompleteCursor, feedWidth-8))
				}
			} else {
				opUnreadDot := ""
				if !m.isMessageSeen(disc.ID, disc.Author, disc.CreatedAt, disc.LastReadAt) {
					opUnreadDot = lipgloss.NewStyle().Foreground(SuccessColor).Bold(true).Render("● ")
				}

				opAuthor := authorStyle.Render("@" + disc.Author)
				opAge := timeStyle.Render(formatTimeAgo(disc.CreatedAt))
				opFormatted := renderMarkdownContent(disc.Body)
				opHeader := fmt.Sprintf("%s%s%s%s %s %s  ", cursorPrefix, flashSymbol, opUnreadDot, opAuthor, dot, opAge)
				opLines := wrapFormattedText(opFormatted, feedWidth-lipgloss.Width(opHeader), feedWidth-4)

				opSelectedTag := ""
				if isCurrentThread && isMsgLevel && m.ThreadsSelectedMsgIdx == 0 {
					opSelectedTag = lipgloss.NewStyle().Foreground(SecondaryColor).Italic(true).Render("  (selected - [e] edit, [c] copy text, [y] links)")
				}

				feedBlocks = append(feedBlocks, opHeader+opLines[0]+opSelectedTag)
				for i := 1; i < len(opLines); i++ {
					feedBlocks = append(feedBlocks, fmt.Sprintf("    %s", opLines[i]))
				}
			}

			// Comments handling
			comments := m.DiscussionComments[disc.ID]
			if len(comments) == 0 && disc.CommentsCount > 0 {
				comments = disc.Comments
			}

			if len(comments) > 0 {
				if !isExpanded {
					// Collapsed
					if len(comments) == 1 {
						cm := comments[0]
						cmUnreadDot := ""
						if !m.isMessageSeen(cm.ID, cm.Author, cm.CreatedAt, disc.LastReadAt) {
							cmUnreadDot = lipgloss.NewStyle().Foreground(SuccessColor).Bold(true).Render("● ")
						}
						cmAuthor := lipgloss.NewStyle().Foreground(lipgloss.Color("#5E8B75")).Bold(true).Render("@" + cm.Author)
						cmAge := timeStyle.Render(formatTimeAgo(cm.CreatedAt))
						cmFormatted := renderMarkdownContent(cm.Body)
						cmHeader := fmt.Sprintf("    %s%s %s %s  ", cmUnreadDot, cmAuthor, dot, cmAge)
						cmLines := wrapFormattedText(cmFormatted, feedWidth-lipgloss.Width(cmHeader), feedWidth-6)
						feedBlocks = append(feedBlocks, cmHeader+cmLines[0])
						for i := 1; i < len(cmLines); i++ {
							feedBlocks = append(feedBlocks, fmt.Sprintf("      %s", cmLines[i]))
						}
					} else {
						expandNote := lipgloss.NewStyle().Foreground(MutedColor).Italic(true).Render(
							fmt.Sprintf("    ↳ %d replies (press Enter or → to expand all)", len(comments)),
						)
						feedBlocks = append(feedBlocks, expandNote)

						lastCm := comments[len(comments)-1]
						lastCmUnreadDot := ""
						if !m.isMessageSeen(lastCm.ID, lastCm.Author, lastCm.CreatedAt, disc.LastReadAt) {
							lastCmUnreadDot = lipgloss.NewStyle().Foreground(SuccessColor).Bold(true).Render("● ")
						}
						cmAuthor := lipgloss.NewStyle().Foreground(lipgloss.Color("#5E8B75")).Bold(true).Render("@" + lastCm.Author)
						cmAge := timeStyle.Render(formatTimeAgo(lastCm.CreatedAt))
						cmFormatted := renderMarkdownContent(lastCm.Body)
						cmHeader := fmt.Sprintf("    %s%s %s %s  ", lastCmUnreadDot, cmAuthor, dot, cmAge)
						cmLines := wrapFormattedText(cmFormatted, feedWidth-lipgloss.Width(cmHeader), feedWidth-6)
						feedBlocks = append(feedBlocks, cmHeader+cmLines[0])
						for i := 1; i < len(cmLines); i++ {
							feedBlocks = append(feedBlocks, fmt.Sprintf("      %s", cmLines[i]))
						}
					}
				} else {
					// Expanded: show all replies indented by 2 spaces from OP
					for cIdx, cm := range comments {
						if m.ThreadsEditActive && m.ThreadEditMessageID == cm.ID {
							m.setupEditInputPrompt(width)
							m.ensureEditComposerScrolledToFit()
							editLine := m.ThreadEditMessageInput.View()
							feedBlocks = append(feedBlocks, editLine)
							if m.AutocompleteActive && len(m.AutocompleteItems) > 0 {
								feedBlocks = append(feedBlocks, "    "+renderAutocompletePopup(m.AutocompleteItems, m.AutocompleteCursor, feedWidth-8))
							}
						} else {
							cmUnreadDot := ""
							if !m.isMessageSeen(cm.ID, cm.Author, cm.CreatedAt, disc.LastReadAt) {
								cmUnreadDot = lipgloss.NewStyle().Foreground(SuccessColor).Bold(true).Render("● ")
							}
							cmAuthor := lipgloss.NewStyle().Foreground(lipgloss.Color("#5E8B75")).Bold(true).Render("@" + cm.Author)
							cmAge := timeStyle.Render(formatTimeAgo(cm.CreatedAt))
							cmFormatted := renderMarkdownContent(cm.Body)

							cmPrefix := "    "
							cmSelectedTag := ""
							if isCurrentThread && isMsgLevel && m.ThreadsSelectedMsgIdx == cIdx+1 {
								cmPrefix = "  " + lipgloss.NewStyle().Foreground(SecondaryColor).Bold(true).Render("▶ ")
								cmSelectedTag = lipgloss.NewStyle().Foreground(SecondaryColor).Italic(true).Render("  (selected - [e] edit, [c] copy text, [y] links)")
							}

							cmHeader := fmt.Sprintf("%s%s%s %s %s  ", cmPrefix, cmUnreadDot, cmAuthor, dot, cmAge)
							cmLines := wrapFormattedText(cmFormatted, feedWidth-lipgloss.Width(cmHeader), feedWidth-6)
							feedBlocks = append(feedBlocks, cmHeader+cmLines[0]+cmSelectedTag)
							for i := 1; i < len(cmLines); i++ {
								feedBlocks = append(feedBlocks, fmt.Sprintf("      %s", cmLines[i]))
							}
						}
					}
				}
			}

			// Inline reply prompt / composer: only shown when replying to THIS thread!
			if m.ThreadsReplyActive && (m.ThreadsExpandedID == disc.ID || m.ThreadsReplyDiscussionID == disc.ID) {
				m.setupReplyInputPrompt(width)
				m.ensureComposerScrolledToFit()
				replyLine := m.ThreadsReplyInput.View()
				feedBlocks = append(feedBlocks, replyLine)
				if m.AutocompleteActive && len(m.AutocompleteItems) > 0 {
					feedBlocks = append(feedBlocks, "    "+renderAutocompletePopup(m.AutocompleteItems, m.AutocompleteCursor, feedWidth-8))
				}
			}

			feedBlocks = append(feedBlocks, "") // Clean spacing between threads
		}
	}

	feedContent := lipgloss.JoinVertical(lipgloss.Left, feedBlocks...)
	return lipgloss.JoinVertical(lipgloss.Left, banner, feedContent)
}

func (m Model) handleThreadsKeys(msg tea.KeyMsg) (Model, tea.Cmd) {
	key := msg.String()

	// 1. Yank Link Active: pick 1..9 or Esc
	if m.ThreadsYankActive {
		if key == "esc" {
			m.ThreadsYankActive = false
			m.ThreadsYankItems = nil
			return m, nil
		}
		if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
			idx := int(key[0] - '1')
			if idx >= 0 && idx < len(m.ThreadsYankItems) {
				item := m.ThreadsYankItems[idx]
				_ = CopyToClipboard(item.Value)
				m.StatusMsg = fmt.Sprintf("✓ Copied to clipboard: %s", item.Label)
				m.StatusMsgTimer = time.Now().Add(3 * time.Second)
				m.ThreadsYankActive = false
				m.ThreadsYankItems = nil
				return m, nil
			}
		}
		return m, nil
	}

	// 2. Inline Reply Active (typing in reply input)
	if m.ThreadsReplyActive {
		if m.AutocompleteActive {
			switch key {
			case "esc":
				m.AutocompleteActive = false
				return m, nil
			case "up", "ctrl+p":
				if len(m.AutocompleteItems) > 0 {
					m.AutocompleteCursor = (m.AutocompleteCursor - 1 + len(m.AutocompleteItems)) % len(m.AutocompleteItems)
				}
				return m, nil
			case "down", "ctrl+n":
				if len(m.AutocompleteItems) > 0 {
					m.AutocompleteCursor = (m.AutocompleteCursor + 1) % len(m.AutocompleteItems)
				}
				return m, nil
			case "tab", "enter":
				if len(m.AutocompleteItems) > 0 {
					selected := m.AutocompleteItems[m.AutocompleteCursor]
					insertVal := selected.InsertText
					if insertVal == "" {
						insertVal = selected.Key
					}
					newVal := insertAutocomplete(m.ThreadsReplyInput.Value(), insertVal)
					m.ThreadsReplyInput.SetValue(newVal)
					m.ThreadsReplyInput.CursorEnd()
					lines := calculateInputVisualLines(m.ThreadsReplyInput.Value(), m.ThreadsReplyInput.Width())
					if lines < 1 {
						lines = 1
					}
					if lines > 12 {
						lines = 12
					}
					m.ThreadsReplyInput.SetHeight(lines)
				}
				m.AutocompleteActive = false
				return m, nil
			}
		}

		switch key {
		case "esc":
			m.ThreadsReplyActive = false
			m.ThreadsReplyDiscussionID = ""
			m.ThreadsReplyInput.Blur()
			m.AutocompleteActive = false
			return m, nil

		case "shift+enter", "alt+enter", "ctrl+j":
			m.ThreadsReplyInput.InsertString("\n")
			lines := calculateInputVisualLines(m.ThreadsReplyInput.Value(), m.ThreadsReplyInput.Width())
			if lines < 1 {
				lines = 1
			}
			if lines > 12 {
				lines = 12
			}
			m.ThreadsReplyInput.SetHeight(lines)
			m.ensureComposerScrolledToFit()
			return m, nil

		case "enter", "ctrl+s":
			replyText := strings.TrimSpace(m.ThreadsReplyInput.Value())
			if replyText == "" {
				m.StatusMsg = "Reply cannot be empty"
				m.StatusMsgTimer = time.Now().Add(2 * time.Second)
				return m, nil
			}
			targetID := m.ThreadsReplyDiscussionID
			if targetID == "" {
				disc := m.getSelectedDiscussion()
				if disc != nil {
					targetID = disc.ID
				}
			}
			if targetID == "" {
				return m, nil
			}
			discNum := 0
			for _, d := range m.Discussions {
				if d.ID == targetID {
					discNum = d.Number
					break
				}
			}
			m.ThreadsReplyInput.Reset()
			m.ThreadsReplyActive = false
			m.ThreadsReplyDiscussionID = ""
			m.AutocompleteActive = false
			m.StatusMsg = fmt.Sprintf("Posting reply to #%d...", discNum)
			m.StatusMsgTimer = time.Now().Add(3 * time.Second)
			return m, m.postThreadReplyCmd(targetID, replyText)
		}

		// Pre-expand height before Update(msg) so repositionView doesn't scroll down when wrapping
		predicted := m.ThreadsReplyInput.Value()
		if len(msg.Runes) > 0 {
			predicted += string(msg.Runes)
		}
		preLines := calculateInputVisualLines(predicted, m.ThreadsReplyInput.Width())
		if preLines < 1 {
			preLines = 1
		}
		if preLines > 12 {
			preLines = 12
		}
		if preLines > m.ThreadsReplyInput.Height() {
			m.ThreadsReplyInput.SetHeight(preLines)
		}

		var cmd tea.Cmd
		m.ThreadsReplyInput, cmd = m.ThreadsReplyInput.Update(msg)
		lines := calculateInputVisualLines(m.ThreadsReplyInput.Value(), m.ThreadsReplyInput.Width())
		if lines < 1 {
			lines = 1
		}
		if lines > 12 {
			lines = 12
		}
		m.ThreadsReplyInput.SetHeight(lines)
		m.ensureComposerScrolledToFit()

		// Check autocomplete
		active, trigger, query := detectAutocomplete(m.ThreadsReplyInput.Value())
		if active {
			items := m.getAutocompleteSuggestions(trigger, query)
			if len(items) > 0 {
				m.AutocompleteActive = true
				m.AutocompleteTrigger = trigger
				m.AutocompleteQuery = query
				m.AutocompleteCursor = 0
				m.AutocompleteItems = items
			} else {
				m.AutocompleteActive = false
			}
		} else {
			m.AutocompleteActive = false
		}
		return m, cmd
	}

	// 3. Inline Message Edit Active (typing in edit textarea)
	if m.ThreadsEditActive {
		// Autocomplete handling
		if m.AutocompleteActive {
			switch key {
			case "esc":
				m.AutocompleteActive = false
				return m, nil
			case "up", "ctrl+p":
				if len(m.AutocompleteItems) > 0 {
					m.AutocompleteCursor = (m.AutocompleteCursor - 1 + len(m.AutocompleteItems)) % len(m.AutocompleteItems)
				}
				return m, nil
			case "down", "ctrl+n":
				if len(m.AutocompleteItems) > 0 {
					m.AutocompleteCursor = (m.AutocompleteCursor + 1) % len(m.AutocompleteItems)
				}
				return m, nil
			case "tab", "enter":
				if len(m.AutocompleteItems) > 0 {
					selected := m.AutocompleteItems[m.AutocompleteCursor]
					insertVal := selected.InsertText
					if insertVal == "" {
						insertVal = selected.Key
					}
					newVal := insertAutocomplete(m.ThreadEditMessageInput.Value(), insertVal)
					m.ThreadEditMessageInput.SetValue(newVal)
					m.ThreadEditMessageInput.CursorEnd()
					lines := calculateInputVisualLines(m.ThreadEditMessageInput.Value(), m.ThreadEditMessageInput.Width())
					if lines < 1 {
						lines = 1
					}
					if lines > 20 {
						lines = 20
					}
					m.ThreadEditMessageInput.SetHeight(lines)
				}
				m.AutocompleteActive = false
				return m, nil
			}
		}

		switch key {
		case "esc":
			m.ThreadsEditActive = false
			m.ThreadEditMessageID = ""
			m.ThreadEditMessageInput.Blur()
			m.AutocompleteActive = false
			return m, nil

		case "shift+enter", "alt+enter", "ctrl+j":
			m.ThreadEditMessageInput.InsertString("\n")
			lines := calculateInputVisualLines(m.ThreadEditMessageInput.Value(), m.ThreadEditMessageInput.Width())
			if lines < 1 {
				lines = 1
			}
			if lines > 20 {
				lines = 20
			}
			m.ThreadEditMessageInput.SetHeight(lines)
			m.ensureEditComposerScrolledToFit()
			return m, nil

		case "enter", "ctrl+s":
			newBody := strings.TrimSpace(m.ThreadEditMessageInput.Value())
			if newBody == "" {
				m.StatusMsg = "Message cannot be empty"
				m.StatusMsgTimer = time.Now().Add(2 * time.Second)
				return m, nil
			}
			discID := m.ThreadEditDiscID
			msgID := m.ThreadEditMessageID
			isOP := m.ThreadEditIsOP
			m.ThreadsEditActive = false
			m.ThreadEditMessageID = ""
			m.ThreadEditMessageInput.Blur()
			m.AutocompleteActive = false
			m.StatusMsg = "Saving message changes..."
			m.StatusMsgTimer = time.Now().Add(3 * time.Second)
			return m, m.updateMessageCmd(discID, msgID, newBody, isOP)
		}

		// Pre-expand height before Update(msg) so repositionView doesn't scroll down when wrapping
		predicted := m.ThreadEditMessageInput.Value()
		if len(msg.Runes) > 0 {
			predicted += string(msg.Runes)
		}
		preLines := calculateInputVisualLines(predicted, m.ThreadEditMessageInput.Width())
		if preLines < 1 {
			preLines = 1
		}
		if preLines > 20 {
			preLines = 20
		}
		if preLines > m.ThreadEditMessageInput.Height() {
			m.ThreadEditMessageInput.SetHeight(preLines)
		}

		var cmd tea.Cmd
		m.ThreadEditMessageInput, cmd = m.ThreadEditMessageInput.Update(msg)
		lines := calculateInputVisualLines(m.ThreadEditMessageInput.Value(), m.ThreadEditMessageInput.Width())
		if lines < 1 {
			lines = 1
		}
		if lines > 20 {
			lines = 20
		}
		m.ThreadEditMessageInput.SetHeight(lines)
		m.ensureEditComposerScrolledToFit()

		// Check autocomplete
		active, trigger, query := detectAutocomplete(m.ThreadEditMessageInput.Value())
		if active {
			items := m.getAutocompleteSuggestions(trigger, query)
			if len(items) > 0 {
				m.AutocompleteActive = true
				m.AutocompleteTrigger = trigger
				m.AutocompleteQuery = query
				m.AutocompleteCursor = 0
				m.AutocompleteItems = items
			} else {
				m.AutocompleteActive = false
			}
		} else {
			m.AutocompleteActive = false
		}
		return m, cmd
	}

	items := m.getThreadFeedItems()
	if len(items) == 0 {
		switch key {
		case "c", "n":
			m.ActiveModal = ModalCreateThread
			if m.ThreadCreateInput.Placeholder == "" {
				m.ThreadCreateInput = textarea.New()
			}
			m.ThreadCreateInput.Reset()
			m.ThreadCreateInput.Focus()
			m.AutocompleteActive = false
			return m, nil
		case "R":
			m.LastDiscussionsSync = time.Now()
			m.StatusMsg = "Refreshing threads from GitHub..."
			m.StatusMsgTimer = time.Now().Add(3 * time.Second)
			return m, tea.Batch(m.loadDiscussionsCmd(), m.Spinner.Tick)
		}
		return m, nil
	}

	if m.ThreadsCursor < 0 {
		m.ThreadsCursor = 0
	}
	if m.ThreadsCursor >= len(items) {
		m.ThreadsCursor = len(items) - 1
	}

	currentItem := items[m.ThreadsCursor]

	// 3. Key handling when cursor is on Tier Header
	if currentItem.Kind == FeedItemTierHeader {
		switch key {
		case "enter", " ":
			if m.ThreadsCollapsedTiers == nil {
				m.ThreadsCollapsedTiers = make(map[ThreadTier]bool)
			}
			isCollapsed := m.isThreadTierCollapsed(currentItem.Tier)
			m.ThreadsCollapsedTiers[currentItem.Tier] = !isCollapsed
			newItems := m.getThreadFeedItems()
			if m.ThreadsCursor >= len(newItems) {
				m.ThreadsCursor = len(newItems) - 1
			}
			action := "Expanded"
			if !isCollapsed {
				action = "Collapsed"
			}
			m.StatusMsg = fmt.Sprintf("%s %s tier", action, currentItem.Tier.String())
			m.StatusMsgTimer = time.Now().Add(2 * time.Second)
			return m, nil

		case "h", "left", "[":
			if m.ThreadsCollapsedTiers == nil {
				m.ThreadsCollapsedTiers = make(map[ThreadTier]bool)
			}
			m.ThreadsCollapsedTiers[currentItem.Tier] = true
			newItems := m.getThreadFeedItems()
			if m.ThreadsCursor >= len(newItems) {
				m.ThreadsCursor = len(newItems) - 1
			}
			m.StatusMsg = fmt.Sprintf("Collapsed %s tier", currentItem.Tier.String())
			m.StatusMsgTimer = time.Now().Add(2 * time.Second)
			return m, nil

		case "l", "right", "]":
			if m.ThreadsCollapsedTiers == nil {
				m.ThreadsCollapsedTiers = make(map[ThreadTier]bool)
			}
			m.ThreadsCollapsedTiers[currentItem.Tier] = false
			m.StatusMsg = fmt.Sprintf("Expanded %s tier", currentItem.Tier.String())
			m.StatusMsgTimer = time.Now().Add(2 * time.Second)
			return m, nil

		case "E":
			if m.ThreadsCollapsedTiers == nil {
				m.ThreadsCollapsedTiers = make(map[ThreadTier]bool)
			}
			for _, t := range []ThreadTier{ThreadTierHot, ThreadTierWarm, ThreadTierCold, ThreadTierDead} {
				m.ThreadsCollapsedTiers[t] = false
			}
			m.StatusMsg = "Expanded all thread tiers"
			m.StatusMsgTimer = time.Now().Add(2 * time.Second)
			return m, nil

		case "C", "Z":
			if m.ThreadsCollapsedTiers == nil {
				m.ThreadsCollapsedTiers = make(map[ThreadTier]bool)
			}
			for _, t := range []ThreadTier{ThreadTierHot, ThreadTierWarm, ThreadTierCold, ThreadTierDead} {
				m.ThreadsCollapsedTiers[t] = true
			}
			newItems := m.getThreadFeedItems()
			if m.ThreadsCursor >= len(newItems) {
				m.ThreadsCursor = len(newItems) - 1
			}
			m.StatusMsg = "Collapsed all thread tiers"
			m.StatusMsgTimer = time.Now().Add(2 * time.Second)
			return m, nil

		case "D":
			if m.ThreadsCollapsedTiers == nil {
				m.ThreadsCollapsedTiers = make(map[ThreadTier]bool)
			}
			deadCollapsed := m.isThreadTierCollapsed(ThreadTierDead)
			m.ThreadsCollapsedTiers[ThreadTierDead] = !deadCollapsed
			m.ThreadsCollapsedTiers[ThreadTierCold] = !deadCollapsed
			m.ThreadsDeadExpanded = deadCollapsed
			if deadCollapsed {
				m.StatusMsg = "Expanded Cold and Dead tiers"
			} else {
				m.StatusMsg = "Collapsed Cold and Dead tiers"
			}
			m.StatusMsgTimer = time.Now().Add(2 * time.Second)
			return m, nil

		case "j", "down":
			if m.ThreadsCursor < len(items)-1 {
				m.ThreadsCursor++
				m.markCurrentThreadViewed()
			}
			return m, nil

		case "k", "up":
			if m.ThreadsCursor > 0 {
				m.ThreadsCursor--
				m.markCurrentThreadViewed()
			}
			return m, nil

		case "c", "n":
			m.ActiveModal = ModalCreateThread
			if m.ThreadCreateInput.Placeholder == "" {
				m.ThreadCreateInput = textarea.New()
			}
			m.ThreadCreateInput.Reset()
			m.ThreadCreateInput.Focus()
			m.AutocompleteActive = false
			return m, nil

		case "R":
			m.LastDiscussionsSync = time.Now()
			m.StatusMsg = "Refreshing threads from GitHub..."
			m.StatusMsgTimer = time.Now().Add(3 * time.Second)
			return m, tea.Batch(m.loadDiscussionsCmd(), m.Spinner.Tick)

		default:
			return m, nil
		}
	}

	// 4. Key handling when cursor is on a Thread
	disc := &currentItem.Discussion.Discussion

	// 4a. Message-Level Navigation (when descended into thread messages)
	if m.ThreadsSelectedMsgIdx >= 0 && disc != nil {
		comments := m.DiscussionComments[disc.ID]
		if len(comments) == 0 {
			comments = disc.Comments
		}
		totalMsgs := 1 + len(comments) // 0 = OP, 1..len(comments) = replies

		switch key {
		case "j", "down":
			if m.ThreadsSelectedMsgIdx < totalMsgs-1 {
				m.ThreadsSelectedMsgIdx++
				m.markCurrentThreadViewed()
			} else if m.ThreadsCursor < len(items)-1 {
				m.ThreadsCursor++
				m.ThreadsSelectedMsgIdx = -1
				m.markCurrentThreadViewed()
			}
			return m, nil

		case "k", "up":
			if m.ThreadsSelectedMsgIdx > 0 {
				m.ThreadsSelectedMsgIdx--
				m.markCurrentThreadViewed()
			} else if m.ThreadsSelectedMsgIdx == 0 {
				m.ThreadsSelectedMsgIdx = -1
			}
			return m, nil

		case "h", "left":
			if disc != nil {
				if m.ThreadsCollapsedIDs == nil {
					m.ThreadsCollapsedIDs = make(map[string]bool)
				}
				m.ThreadsCollapsedIDs[disc.ID] = true
				if m.ThreadsExpandedID == disc.ID {
					m.ThreadsExpandedID = ""
				}
				if m.ThreadsFocused {
					m.ThreadsFocused = false
				}
				m.StatusMsg = "Collapsed thread"
				m.StatusMsgTimer = time.Now().Add(2 * time.Second)
			}
			m.ThreadsSelectedMsgIdx = -1
			return m, nil

		case "esc":
			m.ThreadsSelectedMsgIdx = -1
			return m, nil

		case "c":
			// Yank message content
			var content string
			if m.ThreadsSelectedMsgIdx == 0 {
				content = disc.Body
			} else {
				cIdx := m.ThreadsSelectedMsgIdx - 1
				if cIdx >= 0 && cIdx < len(comments) {
					content = comments[cIdx].Body
				}
			}
			if content != "" {
				_ = CopyToClipboard(content)
				m.StatusMsg = fmt.Sprintf("✓ Copied message #%d content to clipboard", m.ThreadsSelectedMsgIdx+1)
				m.StatusMsgTimer = time.Now().Add(3 * time.Second)
			}
			return m, nil

		case "y":
			// Yank links scoped strictly to this message
			var msgText string
			if m.ThreadsSelectedMsgIdx == 0 {
				msgText = disc.Body
			} else {
				cIdx := m.ThreadsSelectedMsgIdx - 1
				if cIdx >= 0 && cIdx < len(comments) {
					msgText = comments[cIdx].Body
				}
			}
			items := ExtractYankables(msgText, m.CurrentOrg)
			if len(items) == 0 {
				m.StatusMsg = "No links in this message (press [c] to yank content)"
				m.StatusMsgTimer = time.Now().Add(3 * time.Second)
				return m, nil
			}
			if len(items) == 1 {
				_ = CopyToClipboard(items[0].Value)
				m.StatusMsg = fmt.Sprintf("✓ Copied link to clipboard: %s", items[0].Label)
				m.StatusMsgTimer = time.Now().Add(3 * time.Second)
				return m, nil
			}
			m.ActiveModal = ModalYankLinks
			m.ThreadsYankActive = true
			m.ThreadsYankScope = "message"
			m.ThreadsYankItems = items
			m.ThreadsYankCursor = 0
			return m, nil

		case "r":
			m.startReplyToThread(disc)
			return m, nil

		case "enter":
			m.startReplyToThread(disc)
			return m, nil

		case "e":
			// Edit message
			var targetID string
			var targetAuthor string
			var targetBody string
			isOP := (m.ThreadsSelectedMsgIdx <= 0)
			if isOP {
				targetID = disc.ID
				targetAuthor = disc.Author
				targetBody = disc.Body
			} else {
				cIdx := m.ThreadsSelectedMsgIdx - 1
				if cIdx >= 0 && cIdx < len(comments) {
					targetID = comments[cIdx].ID
					targetAuthor = comments[cIdx].Author
					targetBody = comments[cIdx].Body
				}
			}

			if targetID == "" {
				return m, nil
			}

			if m.AuthUser == "" || !strings.EqualFold(targetAuthor, m.AuthUser) {
				m.StatusMsg = "You can only edit your own messages"
				m.StatusMsgTimer = time.Now().Add(3 * time.Second)
				return m, nil
			}

			m.startEditMessage(disc.ID, targetID, targetAuthor, targetBody, isOP)
			return m, nil

		case "t":
			m.ActiveModal = ModalEditThreadTitle
			if m.ThreadEditTitleInput.Placeholder == "" {
				m.ThreadEditTitleInput = textinput.New()
			}
			m.ThreadEditTitleInput.SetValue(disc.Title)
			m.ThreadEditTitleInput.Focus()
			return m, nil

		case "D":
			if m.ThreadsCollapsedTiers == nil {
				m.ThreadsCollapsedTiers = make(map[ThreadTier]bool)
			}
			deadCollapsed := m.isThreadTierCollapsed(ThreadTierDead)
			m.ThreadsCollapsedTiers[ThreadTierDead] = !deadCollapsed
			m.ThreadsCollapsedTiers[ThreadTierCold] = !deadCollapsed
			m.ThreadsDeadExpanded = deadCollapsed
			if deadCollapsed {
				m.StatusMsg = "Expanded Cold and Dead tiers"
			} else {
				m.StatusMsg = "Collapsed Cold and Dead tiers"
			}
			m.StatusMsgTimer = time.Now().Add(3 * time.Second)
			return m, nil

		case "f":
			m.ThreadsFocused = !m.ThreadsFocused
			if m.ThreadsFocused {
				m.ThreadsExpandedID = disc.ID
				m.StatusMsg = "Focused on thread (press [f] or [Esc] to exit focus)"
			} else {
				m.StatusMsg = "Exited focus mode"
			}
			m.StatusMsgTimer = time.Now().Add(3 * time.Second)
			return m, nil
		}

		return m, nil
	}

	// 4b. Thread-Level Navigation
	switch key {
	case "j", "down":
		if m.ThreadsCursor < len(items)-1 {
			m.ThreadsCursor++
			m.markCurrentThreadViewed()
		}
		return m, nil

	case "k", "up":
		if m.ThreadsCursor > 0 {
			m.ThreadsCursor--
			m.markCurrentThreadViewed()
		}
		return m, nil

	case "h", "left":
		if disc != nil {
			hasUnread := currentItem.Discussion.IsUnread || m.isDiscussionUnread(*disc)
			if m.isThreadExpanded(disc.ID, hasUnread) {
				if m.ThreadsCollapsedIDs == nil {
					m.ThreadsCollapsedIDs = make(map[string]bool)
				}
				m.ThreadsCollapsedIDs[disc.ID] = true
				if m.ThreadsExpandedID == disc.ID {
					m.ThreadsExpandedID = ""
				}
				if m.ThreadsFocused {
					m.ThreadsFocused = false
				}
				m.ThreadsSelectedMsgIdx = -1
				m.StatusMsg = "Collapsed thread"
				m.StatusMsgTimer = time.Now().Add(2 * time.Second)
				return m, nil
			}

			// If already collapsed, jump cursor up to parent tier header
			for idx, it := range items {
				if it.Kind == FeedItemTierHeader && it.Tier == currentItem.Tier {
					m.ThreadsCursor = idx
					break
				}
			}
			return m, nil
		}
		return m, nil

	case "l", "right":
		if disc != nil {
			if m.ThreadsCollapsedIDs != nil {
				delete(m.ThreadsCollapsedIDs, disc.ID)
			}
			m.ThreadsExpandedID = disc.ID
			m.ThreadsSelectedMsgIdx = 0 // descend into OP
			m.markCurrentThreadViewed()
			m.StatusMsg = "Selected OP message. [j/k] select messages, [e] edit own msg, [c] copy text, [y] links, [←] collapse, [Esc] back"
			m.StatusMsgTimer = time.Now().Add(3 * time.Second)
		}
		return m, nil

	case "r":
		if disc != nil {
			m.startReplyToThread(disc)
		}
		return m, nil

	case "enter":
		if disc != nil {
			if m.ThreadsCollapsedIDs != nil {
				delete(m.ThreadsCollapsedIDs, disc.ID)
			}
			if m.ThreadsExpandedID == disc.ID && !m.ThreadsReplyActive {
				m.startReplyToThread(disc)
			} else if m.ThreadsExpandedID == disc.ID {
				m.ThreadsExpandedID = ""
				m.ThreadsReplyActive = false
			} else {
				m.ThreadsExpandedID = disc.ID
				m.markCurrentThreadViewed()
				m.startReplyToThread(disc)
			}
		}
		return m, nil

	case "esc":
		if m.ThreadsFocused {
			m.ThreadsFocused = false
			m.StatusMsg = "Exited focus mode"
			m.StatusMsgTimer = time.Now().Add(2 * time.Second)
		} else if m.ThreadsExpandedID != "" {
			m.ThreadsExpandedID = ""
			m.ThreadsReplyActive = false
		}
		return m, nil

	case "c", "n":
		m.ActiveModal = ModalCreateThread
		if m.ThreadCreateInput.Placeholder == "" {
			m.ThreadCreateInput = textarea.New()
		}
		m.ThreadCreateInput.Reset()
		m.ThreadCreateInput.Focus()
		m.AutocompleteActive = false
		return m, nil

	case "e":
		if disc != nil {
			if m.AuthUser == "" || !strings.EqualFold(disc.Author, m.AuthUser) {
				m.StatusMsg = "You can only edit your own messages"
				m.StatusMsgTimer = time.Now().Add(3 * time.Second)
				return m, nil
			}
			m.startEditMessage(disc.ID, disc.ID, disc.Author, disc.Body, true)
		}
		return m, nil

	case "t":
		if disc != nil {
			m.ActiveModal = ModalEditThreadTitle
			if m.ThreadEditTitleInput.Placeholder == "" {
				m.ThreadEditTitleInput = textinput.New()
			}
			m.ThreadEditTitleInput.SetValue(disc.Title)
			m.ThreadEditTitleInput.Focus()
		}
		return m, nil

	case "f":
		if disc != nil {
			m.ThreadsFocused = !m.ThreadsFocused
			if m.ThreadsFocused {
				if m.ThreadsCollapsedIDs != nil {
					delete(m.ThreadsCollapsedIDs, disc.ID)
				}
				m.ThreadsExpandedID = disc.ID
				m.StatusMsg = "Focused on thread (press [f] or [Esc] to exit focus)"
			} else {
				m.StatusMsg = "Exited focus mode"
			}
			m.StatusMsgTimer = time.Now().Add(3 * time.Second)
		}
		return m, nil

	case "y":
		if disc != nil {
			comments := m.DiscussionComments[disc.ID]
			if len(comments) == 0 {
				comments = disc.Comments
			}
			items := ExtractThreadYankables(disc, comments, m.CurrentOrg)
			if len(items) == 0 {
				m.StatusMsg = "No links or mentions found in thread"
				m.StatusMsgTimer = time.Now().Add(3 * time.Second)
				return m, nil
			}
			if len(items) == 1 {
				_ = CopyToClipboard(items[0].Value)
				m.StatusMsg = fmt.Sprintf("✓ Copied link to clipboard: %s", items[0].Label)
				m.StatusMsgTimer = time.Now().Add(3 * time.Second)
				return m, nil
			}
			m.ActiveModal = ModalYankLinks
			m.ThreadsYankActive = true
			m.ThreadsYankScope = "thread"
			m.ThreadsYankItems = items
			m.ThreadsYankCursor = 0
		}
		return m, nil

	case "E":
		if m.ThreadsCollapsedTiers == nil {
			m.ThreadsCollapsedTiers = make(map[ThreadTier]bool)
		}
		for _, t := range []ThreadTier{ThreadTierHot, ThreadTierWarm, ThreadTierCold, ThreadTierDead} {
			m.ThreadsCollapsedTiers[t] = false
		}
		m.StatusMsg = "Expanded all thread tiers"
		m.StatusMsgTimer = time.Now().Add(2 * time.Second)
		return m, nil

	case "C", "Z":
		if m.ThreadsCollapsedTiers == nil {
			m.ThreadsCollapsedTiers = make(map[ThreadTier]bool)
		}
		for _, t := range []ThreadTier{ThreadTierHot, ThreadTierWarm, ThreadTierCold, ThreadTierDead} {
			m.ThreadsCollapsedTiers[t] = true
		}
		newItems := m.getThreadFeedItems()
		if m.ThreadsCursor >= len(newItems) {
			m.ThreadsCursor = len(newItems) - 1
		}
		m.StatusMsg = "Collapsed all thread tiers"
		m.StatusMsgTimer = time.Now().Add(2 * time.Second)
		return m, nil

	case "D":
		if m.ThreadsCollapsedTiers == nil {
			m.ThreadsCollapsedTiers = make(map[ThreadTier]bool)
		}
		deadCollapsed := m.isThreadTierCollapsed(ThreadTierDead)
		m.ThreadsCollapsedTiers[ThreadTierDead] = !deadCollapsed
		m.ThreadsCollapsedTiers[ThreadTierCold] = !deadCollapsed
		m.ThreadsDeadExpanded = deadCollapsed
		if deadCollapsed {
			m.StatusMsg = "Expanded Cold and Dead tiers"
		} else {
			m.StatusMsg = "Collapsed Cold and Dead tiers"
		}
		m.StatusMsgTimer = time.Now().Add(3 * time.Second)
		return m, nil

	case "R":
		m.LastDiscussionsSync = time.Now()
		m.StatusMsg = "Refreshing threads from GitHub..."
		m.StatusMsgTimer = time.Now().Add(3 * time.Second)
		return m, tea.Batch(m.loadDiscussionsCmd(), m.Spinner.Tick)
	}

	return m, nil
}
