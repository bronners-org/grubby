package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"grubby/internal/github"
)

func (m Model) renderCreateThreadModal() string {
	squadName := "General"
	if m.Config != nil && m.Config.ActiveSquad != "" {
		squadName = m.Config.ActiveSquad
	}
	title := ModalTitleStyle.Render(fmt.Sprintf("Start New Thread in Squad [%s]", squadName))

	// Autocomplete popup if active
	var acPopup string
	if m.AutocompleteActive && len(m.AutocompleteItems) > 0 {
		acPopup = renderAutocompletePopup(m.AutocompleteItems, m.AutocompleteCursor, 65) + "\n\n"
	}

	prompt := lipgloss.NewStyle().Bold(true).Foreground(PrimaryColor).Render("Message (first message in thread):")
	inputLine := m.ThreadCreateInput.View()
	helpLine := lipgloss.NewStyle().Foreground(MutedColor).Render("  [Enter / Ctrl+S] Post Thread  •  [@] Squad members  •  [#] Repos/PRs  •  [Esc] Cancel")

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		"",
		acPopup,
		prompt,
		inputLine,
		"",
		helpLine,
	)

	return ModalBoxStyle.Render(content)
}

func (m Model) handleCreateThreadModalKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// 1. If autocomplete popup is active
	if m.AutocompleteActive {
		switch msg.String() {
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
				newVal := insertAutocomplete(m.ThreadCreateInput.Value(), insertVal)
				m.ThreadCreateInput.SetValue(newVal)
				m.ThreadCreateInput.CursorEnd()
			}
			m.AutocompleteActive = false
			return m, nil
		}
	}

	// 2. Normal modal key handling
	switch msg.String() {
	case "esc":
		m.ActiveModal = ModalNone
		m.AutocompleteActive = false
		return m, nil

	case "enter", "ctrl+s":
		body := strings.TrimSpace(m.ThreadCreateInput.Value())
		if body == "" {
			m.StatusMsg = "Message cannot be empty"
			m.StatusMsgTimer = time.Now().Add(2 * time.Second)
			return m, nil
		}
		m.ActiveModal = ModalNone
		m.AutocompleteActive = false
		m.StatusMsg = "Starting new thread..."
		m.StatusMsgTimer = time.Now().Add(3 * time.Second)
		return m, m.postNewThreadCmd(body)
	}

	var cmd tea.Cmd
	m.ThreadCreateInput, cmd = m.ThreadCreateInput.Update(msg)

	// Check for autocomplete trigger
	active, trigger, query := detectAutocomplete(m.ThreadCreateInput.Value())
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

func (m Model) renderEditThreadTitleModal() string {
	disc := m.getSelectedDiscussion()
	headerText := "Edit Thread Title"
	if disc != nil {
		headerText = fmt.Sprintf("Edit Title for #%d", disc.Number)
	}
	title := ModalTitleStyle.Render(headerText)

	inputLine := m.ThreadEditTitleInput.View()
	helpLine := lipgloss.NewStyle().Foreground(MutedColor).Render("  [Enter] Save Title  •  [Esc] Cancel")

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		"",
		inputLine,
		"",
		helpLine,
	)

	return ModalBoxStyle.Render(content)
}

func (m Model) handleEditThreadTitleModalKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.ActiveModal = ModalNone
		return m, nil

	case "enter":
		disc := m.getSelectedDiscussion()
		if disc == nil {
			m.ActiveModal = ModalNone
			return m, nil
		}
		newTitle := strings.TrimSpace(m.ThreadEditTitleInput.Value())
		if newTitle == "" {
			m.StatusMsg = "Title cannot be empty"
			m.StatusMsgTimer = time.Now().Add(2 * time.Second)
			return m, nil
		}
		m.ActiveModal = ModalNone
		m.StatusMsg = fmt.Sprintf("Updating title for #%d...", disc.Number)
		m.StatusMsgTimer = time.Now().Add(3 * time.Second)
		return m, m.updateThreadTitleCmd(disc.ID, newTitle)
	}

	var cmd tea.Cmd
	m.ThreadEditTitleInput, cmd = m.ThreadEditTitleInput.Update(msg)
	return m, cmd
}

func (m Model) renderReplyThreadModal() string {
	disc := m.getSelectedDiscussion()
	headerText := "Reply to Thread"
	if disc != nil {
		headerText = fmt.Sprintf("Reply to #%d: %s", disc.Number, disc.Title)
	}
	title := ModalTitleStyle.Render(headerText)

	inputLine := "Reply: " + m.ThreadsReplyInput.View()
	helpLine := lipgloss.NewStyle().Foreground(MutedColor).Render("  [Enter / Ctrl+S] Post Reply  •  [Esc] Cancel  •  @mentions #repos !prs supported")

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		"",
		inputLine,
		"",
		helpLine,
	)

	return ModalBoxStyle.Render(content)
}

func (m Model) handleReplyThreadModalKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.ActiveModal = ModalNone
		return m, nil

	case "enter", "ctrl+s":
		disc := m.getSelectedDiscussion()
		if disc == nil {
			m.ActiveModal = ModalNone
			return m, nil
		}
		body := strings.TrimSpace(m.ThreadsReplyInput.Value())
		if body == "" {
			m.StatusMsg = "Reply cannot be empty"
			m.StatusMsgTimer = time.Now().Add(2 * time.Second)
			return m, nil
		}
		m.ActiveModal = ModalNone
		m.StatusMsg = fmt.Sprintf("Posting reply to #%d...", disc.Number)
		m.StatusMsgTimer = time.Now().Add(3 * time.Second)
		return m, m.postThreadReplyCmd(disc.ID, body)
	}

	var cmd tea.Cmd
	m.ThreadsReplyInput, cmd = m.ThreadsReplyInput.Update(msg)
	return m, cmd
}

func (m Model) postNewThreadCmd(body string) tea.Cmd {
	return func() tea.Msg {
		if m.GHClient == nil || m.Config == nil {
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		homeOrg := m.Config.HomeOrg
		if homeOrg == "" {
			homeOrg = m.CurrentOrg
		}
		owner, repo := m.Config.GetCentralTasksRepo(homeOrg)

		// 1. Ensure discussions are enabled on the repository
		_, _ = m.GHClient.EnsureDiscussionsEnabled(ctx, owner, repo)

		repoID := m.RepositoryID
		cats := m.DiscussionCategories
		if repoID == "" || len(cats) == 0 {
			rID, fetchedCats, err := m.GHClient.GetDiscussionCategories(ctx, owner, repo)
			if err != nil {
				return StatusToastMsg(fmt.Sprintf("Failed to get categories: %v", err))
			}
			repoID = rID
			cats = fetchedCats
		}

		// Select category matching squad name or slug, or fallback to General/first category
		squad := ""
		if m.Config != nil {
			squad = strings.ToLower(strings.TrimSpace(m.Config.ActiveSquad))
		}

		var catID string
		if squad != "" {
			for _, c := range cats {
				if strings.ToLower(strings.TrimSpace(c.Name)) == squad || strings.ToLower(strings.TrimSpace(c.Slug)) == squad {
					catID = c.ID
					break
				}
			}
		}

		if catID == "" && len(cats) > 0 {
			for _, c := range cats {
				if strings.EqualFold(c.Name, "General") || strings.EqualFold(c.Slug, "general") {
					catID = c.ID
					break
				}
			}
			if catID == "" {
				catID = cats[0].ID
			}
		}

		author := m.AuthUser
		if author == "" {
			author = "user"
		}
		squadName := "General"
		if m.Config != nil && m.Config.ActiveSquad != "" {
			squadName = m.Config.ActiveSquad
		}

		// Initial title following basic convention: Thread by @author in squad <squad>
		initTitle := fmt.Sprintf("Thread by @%s in squad %s", author, squadName)
		newDisc, err := m.GHClient.CreateDiscussion(ctx, repoID, catID, initTitle, body)
		if err != nil {
			return StatusToastMsg(fmt.Sprintf("Failed to create thread: %v", err))
		}

		// Update title with permanent discussion number: Thread #<number> by @author in squad <squad>
		finalTitle := fmt.Sprintf("Thread #%d by @%s in squad %s", newDisc.Number, author, squadName)
		if updated, err := m.GHClient.UpdateDiscussionTitle(ctx, newDisc.ID, finalTitle); err == nil && updated != nil {
			newDisc.Title = updated.Title
		} else {
			newDisc.Title = finalTitle
		}

		return ThreadCreatedMsg{Discussion: *newDisc}
	}
}

func (m Model) postThreadReplyCmd(discussionID, body string) tea.Cmd {
	return func() tea.Msg {
		if m.GHClient == nil {
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		cm, err := m.GHClient.AddDiscussionComment(ctx, discussionID, body)
		if err != nil {
			return StatusToastMsg(fmt.Sprintf("Failed to post reply: %v", err))
		}

		return ThreadReplyCreatedMsg{DiscussionID: discussionID, Comment: *cm}
	}
}

func (m Model) updateThreadTitleCmd(discussionID, newTitle string) tea.Cmd {
	return func() tea.Msg {
		if m.GHClient == nil {
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		disc, err := m.GHClient.UpdateDiscussionTitle(ctx, discussionID, newTitle)
		if err != nil {
			return StatusToastMsg(fmt.Sprintf("Failed to update title: %v", err))
		}

		return ThreadTitleUpdatedMsg{DiscussionID: discussionID, Title: disc.Title}
	}
}

type ThreadCreatedMsg struct {
	Discussion github.Discussion
}

type ThreadReplyCreatedMsg struct {
	DiscussionID string
	Comment      github.DiscussionComment
}

type ThreadTitleUpdatedMsg struct {
	DiscussionID string
	Title        string
}

type ThreadMessageUpdatedMsg struct {
	DiscID    string
	MessageID string
	Body      string
	IsOP      bool
	UpdatedAt time.Time
}

func (m Model) renderEditMessageModal() string {
	disc := m.getSelectedDiscussion()
	discNum := 0
	if disc != nil {
		discNum = disc.Number
	}
	targetType := "Reply"
	if m.ThreadEditIsOP {
		targetType = "OP Message"
	}
	title := ModalTitleStyle.Render(fmt.Sprintf("Edit %s in Thread #%d (by @%s)", targetType, discNum, m.ThreadEditMessageAuthor))

	// Scale edit input width to match available modal width
	modalWidth := m.Width - 10
	if modalWidth < 70 {
		modalWidth = 70
	}
	m.ThreadEditMessageInput.SetWidth(modalWidth - 4)
	lines := calculateInputVisualLines(m.ThreadEditMessageInput.Value(), m.ThreadEditMessageInput.Width())
	if lines < 2 {
		lines = 2
	}
	if lines > 18 {
		lines = 18
	}
	m.ThreadEditMessageInput.SetHeight(lines)

	// Autocomplete popup if active
	var acPopup string
	if m.AutocompleteActive && len(m.AutocompleteItems) > 0 {
		acPopup = renderAutocompletePopup(m.AutocompleteItems, m.AutocompleteCursor, modalWidth-6) + "\n\n"
	}

	prompt := lipgloss.NewStyle().Bold(true).Foreground(PrimaryColor).Render("Message Content:")
	inputLine := m.ThreadEditMessageInput.View()
	helpLine := lipgloss.NewStyle().Foreground(MutedColor).Render("  [Enter / Ctrl+S] Save Changes  •  [Shift+Enter] Line Break  •  [@] Squad  •  [#] Refs  •  [Esc] Cancel")

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		"",
		acPopup,
		prompt,
		inputLine,
		"",
		helpLine,
	)

	return ModalBoxStyle.Width(modalWidth).Render(content)
}

func (m Model) handleEditMessageModalKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// 1. If autocomplete popup is active
	if m.AutocompleteActive {
		switch msg.String() {
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
				if lines < 2 {
					lines = 2
				}
				if lines > 18 {
					lines = 18
				}
				m.ThreadEditMessageInput.SetHeight(lines)
			}
			m.AutocompleteActive = false
			return m, nil
		}
	}

	// 2. Normal modal key handling
	switch msg.String() {
	case "esc":
		m.ActiveModal = ModalNone
		m.AutocompleteActive = false
		return m, nil

	case "shift+enter", "alt+enter", "ctrl+j":
		m.ThreadEditMessageInput.InsertString("\n")
		lines := calculateInputVisualLines(m.ThreadEditMessageInput.Value(), m.ThreadEditMessageInput.Width())
		if lines < 2 {
			lines = 2
		}
		if lines > 18 {
			lines = 18
		}
		m.ThreadEditMessageInput.SetHeight(lines)
		return m, nil

	case "enter", "ctrl+s":
		newBody := strings.TrimSpace(m.ThreadEditMessageInput.Value())
		if newBody == "" {
			m.StatusMsg = "Message cannot be empty"
			m.StatusMsgTimer = time.Now().Add(2 * time.Second)
			return m, nil
		}
		m.ActiveModal = ModalNone
		m.AutocompleteActive = false
		m.StatusMsg = "Saving message changes..."
		m.StatusMsgTimer = time.Now().Add(3 * time.Second)
		return m, m.updateMessageCmd(m.ThreadEditDiscID, m.ThreadEditMessageID, newBody, m.ThreadEditIsOP)
	}

	// Pre-expand height before Update(msg)
	predicted := m.ThreadEditMessageInput.Value()
	if len(msg.Runes) > 0 {
		predicted += string(msg.Runes)
	}
	preLines := calculateInputVisualLines(predicted, m.ThreadEditMessageInput.Width())
	if preLines < 2 {
		preLines = 2
	}
	if preLines > 18 {
		preLines = 18
	}
	if preLines > m.ThreadEditMessageInput.Height() {
		m.ThreadEditMessageInput.SetHeight(preLines)
	}

	var cmd tea.Cmd
	m.ThreadEditMessageInput, cmd = m.ThreadEditMessageInput.Update(msg)
	lines := calculateInputVisualLines(m.ThreadEditMessageInput.Value(), m.ThreadEditMessageInput.Width())
	if lines < 2 {
		lines = 2
	}
	if lines > 18 {
		lines = 18
	}
	m.ThreadEditMessageInput.SetHeight(lines)

	// Check autocomplete trigger
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

func (m Model) updateMessageCmd(discID, messageID, newBody string, isOP bool) tea.Cmd {
	return func() tea.Msg {
		if m.GHClient == nil {
			return StatusToastMsg("GitHub client not configured")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		var updatedAt time.Time
		if isOP {
			disc, err := m.GHClient.UpdateDiscussionBody(ctx, messageID, newBody)
			if err != nil {
				return StatusToastMsg(fmt.Sprintf("Failed to update discussion: %v", err))
			}
			updatedAt = disc.UpdatedAt
		} else {
			cm, err := m.GHClient.UpdateDiscussionComment(ctx, messageID, newBody)
			if err != nil {
				return StatusToastMsg(fmt.Sprintf("Failed to update comment: %v", err))
			}
			updatedAt = cm.UpdatedAt
		}

		if updatedAt.IsZero() {
			updatedAt = time.Now()
		}

		if m.DB != nil {
			if isOP {
				_ = m.DB.UpdateDiscussionBody(messageID, newBody)
			} else {
				_ = m.DB.UpdateDiscussionCommentBody(messageID, newBody)
			}
		}

		return ThreadMessageUpdatedMsg{
			DiscID:    discID,
			MessageID: messageID,
			Body:      newBody,
			IsOP:      isOP,
			UpdatedAt: updatedAt,
		}
	}
}

func (m Model) renderYankLinksModal() string {
	scopeName := "Thread"
	disc := m.getSelectedDiscussion()
	if m.ThreadsYankScope == "message" {
		scopeName = "Message"
	} else if disc != nil {
		scopeName = fmt.Sprintf("Thread #%d", disc.Number)
	}

	title := ModalTitleStyle.Render(fmt.Sprintf("📋 Yank Links from %s (%d links)", scopeName, len(m.ThreadsYankItems)))

	if len(m.ThreadsYankItems) == 0 {
		content := lipgloss.JoinVertical(
			lipgloss.Left,
			title,
			"",
			lipgloss.NewStyle().Foreground(MutedColor).Render("No links found in selection."),
			"",
			lipgloss.NewStyle().Foreground(MutedColor).Render("  [Esc] Close"),
		)
		return ModalBoxStyle.Render(content)
	}

	var rows []string
	maxVisible := 10
	cursor := m.ThreadsYankCursor
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= len(m.ThreadsYankItems) {
		cursor = len(m.ThreadsYankItems) - 1
	}

	start := 0
	if cursor >= maxVisible {
		start = cursor - maxVisible + 1
	}
	end := start + maxVisible
	if end > len(m.ThreadsYankItems) {
		end = len(m.ThreadsYankItems)
	}

	for i := start; i < end; i++ {
		item := m.ThreadsYankItems[i]
		isCur := (i == cursor)

		prefix := "   "
		keyBadge := ""
		if i < 9 {
			keyBadge = lipgloss.NewStyle().Foreground(SecondaryColor).Bold(true).Render(fmt.Sprintf("[%d] ", i+1))
		} else {
			keyBadge = "    "
		}

		style := lipgloss.NewStyle().Foreground(lipgloss.Color("#E0E0E0"))
		if isCur {
			prefix = lipgloss.NewStyle().Foreground(SecondaryColor).Bold(true).Render("▶ ")
			style = lipgloss.NewStyle().Foreground(SecondaryColor).Bold(true)
		}

		labelStyled := style.Render(item.Label)
		valStyled := lipgloss.NewStyle().Foreground(lipgloss.Color("#708090")).Render(runewidth.Truncate(item.Value, 50, "…"))
		rows = append(rows, fmt.Sprintf("%s%s%-28s  %s", prefix, keyBadge, labelStyled, valStyled))
	}

	scrollNote := ""
	if len(m.ThreadsYankItems) > maxVisible {
		scrollNote = lipgloss.NewStyle().Foreground(MutedColor).Italic(true).Render(
			fmt.Sprintf("  (showing %d-%d of %d links - scroll with ↑/↓/j/k)", start+1, end, len(m.ThreadsYankItems)),
		)
	}

	helpLine := lipgloss.NewStyle().Foreground(MutedColor).Render(
		"  [1..9] Quick Yank  •  [j/k, ↑/↓] Select  •  [Enter] Yank  •  [o] Open Browser  •  [Esc] Cancel",
	)

	itemsBlock := lipgloss.JoinVertical(lipgloss.Left, rows...)
	contentParts := []string{title, ""}
	if scrollNote != "" {
		contentParts = append(contentParts, scrollNote)
	}
	contentParts = append(contentParts, itemsBlock, "", helpLine)

	content := lipgloss.JoinVertical(lipgloss.Left, contentParts...)
	return ModalBoxStyle.Render(content)
}

func (m Model) handleYankLinksModalKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc", "q":
		m.ActiveModal = ModalNone
		m.ThreadsYankActive = false
		m.ThreadsYankItems = nil
		return m, nil

	case "j", "down":
		if m.ThreadsYankCursor < len(m.ThreadsYankItems)-1 {
			m.ThreadsYankCursor++
		}
		return m, nil

	case "k", "up":
		if m.ThreadsYankCursor > 0 {
			m.ThreadsYankCursor--
		}
		return m, nil

	case "enter":
		if m.ThreadsYankCursor >= 0 && m.ThreadsYankCursor < len(m.ThreadsYankItems) {
			item := m.ThreadsYankItems[m.ThreadsYankCursor]
			_ = CopyToClipboard(item.Value)
			m.ActiveModal = ModalNone
			m.ThreadsYankActive = false
			m.ThreadsYankItems = nil
			m.StatusMsg = fmt.Sprintf("✓ Copied to clipboard: %s", item.Label)
			m.StatusMsgTimer = time.Now().Add(3 * time.Second)
		}
		return m, nil

	case "o":
		if m.ThreadsYankCursor >= 0 && m.ThreadsYankCursor < len(m.ThreadsYankItems) {
			item := m.ThreadsYankItems[m.ThreadsYankCursor]
			_ = OpenBrowser(item.Value)
			m.ActiveModal = ModalNone
			m.ThreadsYankActive = false
			m.ThreadsYankItems = nil
			m.StatusMsg = fmt.Sprintf("Opened in browser: %s", item.Label)
			m.StatusMsgTimer = time.Now().Add(3 * time.Second)
		}
		return m, nil
	}

	// 1..9 quick yank
	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		idx := int(key[0] - '1')
		if idx >= 0 && idx < len(m.ThreadsYankItems) {
			item := m.ThreadsYankItems[idx]
			_ = CopyToClipboard(item.Value)
			m.ActiveModal = ModalNone
			m.ThreadsYankActive = false
			m.ThreadsYankItems = nil
			m.StatusMsg = fmt.Sprintf("✓ Copied to clipboard: %s", item.Label)
			m.StatusMsgTimer = time.Now().Add(3 * time.Second)
			return m, nil
		}
	}

	return m, nil
}
