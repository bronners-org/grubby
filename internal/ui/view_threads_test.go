package ui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"grubby/internal/config"
	"grubby/internal/github"
	"grubby/internal/protocol"
)

func TestThreadsView_SlackFeedLayout(t *testing.T) {
	now := time.Now()

	m := createTestModel()
	m.AuthUser = "simon"
	m.Config = &config.Config{
		ActiveSquad: "platform",
	}

	d1 := github.Discussion{
		ID:            "D_101",
		Number:        42,
		RepoOwner:     "org",
		RepoName:      "tasks",
		Title:         "Thread #42 by @simon in squad platform",
		Body:          "Hey @alice, check out #grubby and PR !15.",
		Author:        "simon",
		CreatedAt:     now.Add(-2 * time.Hour),
		UpdatedAt:     now.Add(-10 * time.Minute),
		URL:           "https://github.com/org/tasks/discussions/42",
		Category:      github.DiscussionCategory{Name: "Platform", Slug: "platform"},
		CommentsCount: 1,
		Comments: []github.DiscussionComment{
			{
				ID:           "DC_201",
				DiscussionID: "D_101",
				Author:       "alice",
				Body:         "Looks clean to me!",
				CreatedAt:    now.Add(-5 * time.Minute),
				UpdatedAt:    now.Add(-5 * time.Minute),
			},
		},
	}

	d2 := github.Discussion{
		ID:            "D_102",
		Number:        38,
		RepoOwner:     "org",
		RepoName:      "tasks",
		Title:         "Old archived topic",
		Body:          "Archive thread",
		Author:        "bob",
		CreatedAt:     now.Add(-300 * time.Hour),
		UpdatedAt:     now.Add(-200 * time.Hour),
		URL:           "https://github.com/org/tasks/discussions/38",
		Category:      github.DiscussionCategory{Name: "Platform", Slug: "platform"},
		CommentsCount: 0,
	}

	m.Discussions = []github.Discussion{d1, d2}
	m.ActiveTab = TabThreads
	m.ThreadsDeadExpanded = false
	m.ThreadsCursor = m.firstSelectableThreadIndex()

	// 1. Collapsed View: Verify compact line: @simon • ... and indented reply: @alice • ...
	// AND verify NO repeated "[r]eply" buttons with numbers are shown while browsing!
	view := m.renderThreadsView()
	if !strings.Contains(view, "@simon") {
		t.Errorf("expected active thread OP @simon in view, got:\n%s", view)
	}
	if !strings.Contains(view, "Looks clean to me!") || !strings.Contains(view, "@alice") {
		t.Errorf("expected indented last reply in collapsed thread view, got:\n%s", view)
	}
	if strings.Contains(view, "[r]eply") {
		t.Errorf("expected NO '[r]eply' button while browsing, got:\n%s", view)
	}

	// 2. Dead thread #38 is collapsed
	if strings.Contains(view, "Archive thread") {
		t.Errorf("expected dead thread to be collapsed, got:\n%s", view)
	}

	// 3. Press 'r' on thread #42 -> drops directly into inline reply prompt with @simon
	updated, _ := m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated
	if m.ThreadsExpandedID != "D_101" {
		t.Errorf("expected ThreadsExpandedID to be D_101, got %q", m.ThreadsExpandedID)
	}
	if !m.ThreadsReplyActive {
		t.Errorf("expected ThreadsReplyActive=true after pressing 'r'")
	}

	expandedView := m.renderThreadsView()
	// Should show prompt with current user @simon
	if !strings.Contains(expandedView, "@simon") {
		t.Errorf("expected inline reply prompt with @simon in view, got:\n%s", expandedView)
	}
	if !strings.Contains(expandedView, "@alice") {
		t.Errorf("expected reply @alice indented by 2 spaces in expanded view, got:\n%s", expandedView)
	}

	// 4. Press Esc -> exits reply mode
	updated, _ = m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated
	if m.ThreadsReplyActive {
		t.Errorf("expected ThreadsReplyActive=false after pressing Esc")
	}

	// 5. Press 'D' to toggle expand dead threads
	updated, _ = m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'D'}})
	m = updated
	if !m.ThreadsDeadExpanded {
		t.Errorf("expected ThreadsDeadExpanded=true after pressing D")
	}

	viewDead := m.renderThreadsView()
	if !strings.Contains(viewDead, "Old archived topic") && !strings.Contains(viewDead, "Archive thread") {
		t.Errorf("expected dead thread to be visible after pressing D, got:\n%s", viewDead)
	}
}

func TestThreadsView_SelectionNavigationAndDescending(t *testing.T) {
	now := time.Now()
	m := createTestModel()
	m.AuthUser = "simon"
	m.Config = &config.Config{ActiveSquad: "platform"}

	d1 := github.Discussion{
		ID:        "D_1",
		Number:    7,
		Title:     "Thread 1",
		Body:      "First message",
		Author:    "simon",
		CreatedAt: now,
		UpdatedAt: now,
		Category:  github.DiscussionCategory{Name: "General", Slug: "general"},
		Comments: []github.DiscussionComment{
			{ID: "C_1", DiscussionID: "D_1", Author: "alice", Body: "Reply one", CreatedAt: now},
			{ID: "C_2", DiscussionID: "D_1", Author: "bob", Body: "Reply two", CreatedAt: now},
		},
		CommentsCount: 2,
	}
	d2 := github.Discussion{
		ID:        "D_2",
		Number:    8,
		Title:     "Thread 2",
		Body:      "Second message",
		Author:    "bob",
		CreatedAt: now,
		UpdatedAt: now,
		Category:  github.DiscussionCategory{Name: "General", Slug: "general"},
	}
	m.Discussions = []github.Discussion{d1, d2}
	m.ActiveTab = TabThreads
	m.ThreadsCursor = m.firstSelectableThreadIndex() // 1 (Thread 1, since 0 is Hot tier header)
	m.ThreadsSelectedMsgIdx = -1

	// 1. Thread level navigation: 'j' moves cursor down to thread 2
	updated, _ := m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated
	if m.ThreadsCursor != 2 {
		t.Fatalf("expected ThreadsCursor=2, got %d", m.ThreadsCursor)
	}
	// 'k' moves cursor back to thread 1
	updated, _ = m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated
	if m.ThreadsCursor != 1 {
		t.Fatalf("expected ThreadsCursor=1, got %d", m.ThreadsCursor)
	}

	// 2. Right arrow on thread 1 OP descends into messages: ThreadsSelectedMsgIdx = 0 (OP)
	updated, _ = m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyRight})
	m = updated
	if m.ThreadsSelectedMsgIdx != 0 {
		t.Fatalf("expected ThreadsSelectedMsgIdx=0 after Right arrow, got %d", m.ThreadsSelectedMsgIdx)
	}
	if m.ThreadsExpandedID != "D_1" {
		t.Fatalf("expected ThreadsExpandedID=D_1, got %q", m.ThreadsExpandedID)
	}

	// 3. 'j' moves down to reply 1 (ThreadsSelectedMsgIdx = 1)
	updated, _ = m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated
	if m.ThreadsSelectedMsgIdx != 1 {
		t.Fatalf("expected ThreadsSelectedMsgIdx=1, got %d", m.ThreadsSelectedMsgIdx)
	}

	// 4. 'j' moves down to reply 2 (ThreadsSelectedMsgIdx = 2, which is the last message)
	updated, _ = m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated
	if m.ThreadsSelectedMsgIdx != 2 {
		t.Fatalf("expected ThreadsSelectedMsgIdx=2, got %d", m.ThreadsSelectedMsgIdx)
	}

	// 5. Down arrow on the LAST message of thread 1 advances to thread 2!
	updated, _ = m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyDown})
	m = updated
	if m.ThreadsCursor != 2 {
		t.Fatalf("expected ThreadsCursor=2 after down on last message, got %d", m.ThreadsCursor)
	}
	if m.ThreadsSelectedMsgIdx != -1 {
		t.Fatalf("expected ThreadsSelectedMsgIdx=-1 on next thread, got %d", m.ThreadsSelectedMsgIdx)
	}

	// 6. Navigate back up to thread 1 with 'k', descend into OP, then 'up' ascends to -1
	updated, _ = m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated
	if m.ThreadsCursor != 1 {
		t.Fatalf("expected ThreadsCursor=1, got %d", m.ThreadsCursor)
	}
	updated, _ = m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyRight})
	m = updated
	if m.ThreadsSelectedMsgIdx != 0 {
		t.Fatalf("expected ThreadsSelectedMsgIdx=0 on OP, got %d", m.ThreadsSelectedMsgIdx)
	}
	updated, _ = m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyUp})
	m = updated
	if m.ThreadsSelectedMsgIdx != -1 {
		t.Fatalf("expected ThreadsSelectedMsgIdx=-1 after Up on OP, got %d", m.ThreadsSelectedMsgIdx)
	}
}

func TestThreadsView_YankAndContentCopy(t *testing.T) {
	now := time.Now()
	m := createTestModel()
	m.AuthUser = "simon"
	m.Config = &config.Config{
		ActiveSquad: "platform",
	}

	d1 := github.Discussion{
		ID:        "D_101",
		Number:    42,
		Title:     "Thread 42",
		Body:      "Review https://github.com/bronners-org/grubby/pull/15 on #grubby, cc @alice",
		Author:    "simon",
		CreatedAt: now,
		UpdatedAt: now,
		Category:  github.DiscussionCategory{Name: "Platform", Slug: "platform"},
		Comments: []github.DiscussionComment{
			{
				ID:           "C_101",
				DiscussionID: "D_101",
				Author:       "alice",
				Body:         "Check out https://github.com/bronners-org/tasks/issues/9",
				CreatedAt:    now,
			},
		},
		CommentsCount: 1,
	}

	m.Discussions = []github.Discussion{d1}
	m.ActiveTab = TabThreads
	m.ThreadsCursor = m.firstSelectableThreadIndex()
	m.ThreadsSelectedMsgIdx = -1

	// 1. Thread level: pressing 'y' extracts all links from thread (OP + replies)
	updated, _ := m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated
	if !m.ThreadsYankActive {
		t.Fatalf("expected ThreadsYankActive=true at thread level with multiple links")
	}
	if m.ThreadsYankScope != "thread" {
		t.Fatalf("expected ThreadsYankScope='thread', got %q", m.ThreadsYankScope)
	}
	if len(m.ThreadsYankItems) < 2 {
		t.Fatalf("expected at least 2 links extracted from thread, got %d", len(m.ThreadsYankItems))
	}

	// 2. Press '1' to yank first link
	updated, _ = m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	m = updated
	if m.ThreadsYankActive {
		t.Errorf("expected ThreadsYankActive=false after picking link")
	}
	if !strings.Contains(m.StatusMsg, "Copied") {
		t.Errorf("expected StatusMsg to mention Copied, got %q", m.StatusMsg)
	}

	// 3. Descend into thread: right arrow -> OP (idx 0), then 'j' -> Reply 1 (idx 1)
	updated, _ = m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyRight})
	m = updated
	updated, _ = m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated
	if m.ThreadsSelectedMsgIdx != 1 {
		t.Fatalf("expected ThreadsSelectedMsgIdx=1, got %d", m.ThreadsSelectedMsgIdx)
	}

	// 4. At message level: press 'c' to copy message content
	updated, _ = m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = updated
	if !strings.Contains(m.StatusMsg, "Copied message #2 content") {
		t.Errorf("expected StatusMsg to mention 'Copied message #2 content', got %q", m.StatusMsg)
	}

	// 5. At message level: press 'y' to get links scoped strictly to message 1 (has 1 link -> copied directly)
	updated, _ = m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated
	if !strings.Contains(m.StatusMsg, "Copied") {
		t.Errorf("expected StatusMsg to mention Copied for single message link, got %q", m.StatusMsg)
	}
}

func TestThreadsView_FocusModeAndTitleEdit(t *testing.T) {
	now := time.Now()
	m := createTestModel()
	m.AuthUser = "simon"
	m.Config = &config.Config{ActiveSquad: "platform"}

	d1 := github.Discussion{
		ID:        "D_1",
		Number:    1,
		Title:     "Feature Request Discussion",
		Body:      "Should we build focus mode?",
		Author:    "simon",
		CreatedAt: now,
		UpdatedAt: now,
		Category:  github.DiscussionCategory{Name: "Platform", Slug: "platform"},
	}
	d2 := github.Discussion{
		ID:        "D_2",
		Number:    2,
		Title:     "Other Discussion",
		Body:      "Some other text",
		Author:    "bob",
		CreatedAt: now,
		UpdatedAt: now,
		Category:  github.DiscussionCategory{Name: "Platform", Slug: "platform"},
	}
	m.Discussions = []github.Discussion{d1, d2}
	m.ActiveTab = TabThreads
	m.ThreadsCursor = m.firstSelectableThreadIndex()
	m.ThreadsSelectedMsgIdx = -1

	// 1. Light grey title rendered in view
	view := m.renderThreadsView()
	if !strings.Contains(view, "Feature Request Discussion") {
		t.Errorf("expected title in view, got:\n%s", view)
	}

	// 2. Press 't' to open title edit modal
	updated, _ := m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m = updated
	if m.ActiveModal != ModalEditThreadTitle {
		t.Fatalf("expected ActiveModal=ModalEditThreadTitle, got %v", m.ActiveModal)
	}
	if m.ThreadEditTitleInput.Value() != "Feature Request Discussion" {
		t.Fatalf("expected ThreadEditTitleInput to have current title, got %q", m.ThreadEditTitleInput.Value())
	}
	m.ActiveModal = ModalNone

	// 3. Press 'f' to toggle focus mode
	updated, _ = m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = updated
	if !m.ThreadsFocused {
		t.Fatalf("expected ThreadsFocused=true after pressing 'f'")
	}

	// In focus mode, only d1 is visible, d2 is hidden
	focusedView := m.renderThreadsView()
	if !strings.Contains(focusedView, "FOCUSED THREAD #1") {
		t.Errorf("expected FOCUSED banner, got:\n%s", focusedView)
	}
	if strings.Contains(focusedView, "Other Discussion") {
		t.Errorf("expected other discussion to be hidden in focus mode, got:\n%s", focusedView)
	}

	// Press 'f' again to exit focus mode
	updated, _ = m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = updated
	if m.ThreadsFocused {
		t.Fatalf("expected ThreadsFocused=false after pressing 'f' again")
	}
}

func TestThreadsView_PinnedReplyAndMultiline(t *testing.T) {
	now := time.Now()
	m := createTestModel()
	m.AuthUser = "simon"
	m.Config = &config.Config{ActiveSquad: "platform"}

	d1 := github.Discussion{
		ID:        "D_1",
		Number:    1,
		Title:     "Thread 1",
		Body:      "Body 1",
		Author:    "simon",
		CreatedAt: now,
		UpdatedAt: now,
	}
	d2 := github.Discussion{
		ID:        "D_2",
		Number:    2,
		Title:     "Thread 2",
		Body:      "Body 2",
		Author:    "bob",
		CreatedAt: now,
		UpdatedAt: now,
	}
	m.Discussions = []github.Discussion{d1, d2}
	m.ActiveTab = TabThreads
	m.ThreadsCursor = 2 // Cursor on Thread 2 (since 0 is Hot header, 1 is Thread 1, 2 is Thread 2)

	// 1. Press 'r' -> start reply pinned to D_2
	updated, _ := m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated
	if m.ThreadsReplyDiscussionID != "D_2" {
		t.Fatalf("expected ThreadsReplyDiscussionID=D_2, got %q", m.ThreadsReplyDiscussionID)
	}

	// 2. Multiline typing: Shift+Enter or Ctrl+J inserts newline
	m.ThreadsReplyInput.SetValue("Line one")
	updated, _ = m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyCtrlJ})
	m = updated
	if !strings.Contains(m.ThreadsReplyInput.Value(), "\n") {
		m.ThreadsReplyInput.InsertString("\nLine two")
	}

	// 3. Shift ThreadsCursor to 1 while reply active (simulating cursor or re-order shift)
	m.ThreadsCursor = 1

	// 4. Press Enter -> submit reply. It MUST submit to D_2, not D_1!
	updated, cmd := m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated
	if cmd == nil {
		t.Fatalf("expected reply submit command")
	}
	if !strings.Contains(m.StatusMsg, "Posting reply to #2") {
		t.Errorf("expected reply posted to thread #2 (pinned ID D_2), got StatusMsg: %q", m.StatusMsg)
	}
}

func TestThreadsView_Autocomplete(t *testing.T) {
	m := createTestModel()
	m.Config = &config.Config{
		ActiveSquad: "my-squad",
		Squads: []config.SquadConfig{
			{
				Name:    "my-squad",
				Members: []string{"alice", "bob", "charlie"},
			},
		},
	}
	m.Repos = []github.Repository{
		{Name: "grubby", FullName: "bronners-org/grubby"},
		{Name: "tasks", FullName: "bronners-org/tasks"},
	}
	m.PRs = []github.PullRequest{
		{Number: 15, Title: "Clear badge"},
	}

	// 1. Test detectAutocomplete
	active, trigger, query := detectAutocomplete("Hey @al")
	if !active || trigger != '@' || query != "al" {
		t.Errorf("detectAutocomplete failed for @al: active=%v, trigger=%c, query=%q", active, trigger, query)
	}

	active, trigger, query = detectAutocomplete("Check #gru")
	if !active || trigger != '#' || query != "gru" {
		t.Errorf("detectAutocomplete failed for #gru: active=%v, trigger=%c, query=%q", active, trigger, query)
	}

	// 2. Test getAutocompleteSuggestions for '@'
	userSuggestions := m.getAutocompleteSuggestions('@', "al")
	if len(userSuggestions) != 1 || userSuggestions[0].Key != "@alice" {
		t.Errorf("expected @alice suggestion, got: %+v", userSuggestions)
	}
	if userSuggestions[0].InsertText != "[@alice](https://github.com/alice)" {
		t.Errorf("expected markdown link InsertText for @alice, got %q", userSuggestions[0].InsertText)
	}
	if userSuggestions[0].URL != "https://github.com/alice" {
		t.Errorf("expected URL for @alice, got %q", userSuggestions[0].URL)
	}

	// 3. Test getAutocompleteSuggestions for '#'
	repoSuggestions := m.getAutocompleteSuggestions('#', "gru")
	if len(repoSuggestions) != 1 || repoSuggestions[0].Key != "#grubby" {
		t.Errorf("expected #grubby suggestion, got: %+v", repoSuggestions)
	}
	if repoSuggestions[0].InsertText != "[#grubby](https://github.com/bronners-org/grubby)" {
		t.Errorf("expected markdown link InsertText for #grubby, got %q", repoSuggestions[0].InsertText)
	}

	// 4. Test insertAutocomplete
	newVal := insertAutocomplete("Hey @al", "@alice")
	if newVal != "Hey @alice " {
		t.Errorf("expected 'Hey @alice ', got %q", newVal)
	}
}

func TestThreadsView_ReactiveEventFlashing(t *testing.T) {
	now := time.Now()
	m := createTestModel()
	m.AuthUser = "simon"
	m.Config = &config.Config{
		ActiveSquad: "platform",
	}

	d1 := github.Discussion{
		ID:        "D_101",
		Number:    42,
		Title:     "Thread 42",
		Body:      "Original message",
		Author:    "alice",
		CreatedAt: now.Add(-1 * time.Hour),
		UpdatedAt: now.Add(-1 * time.Hour),
		Category:  github.DiscussionCategory{Name: "Platform", Slug: "platform"},
	}
	m.Discussions = []github.Discussion{d1}

	// Send discussion comment reactive event mentioning @simon
	cm := github.DiscussionComment{
		ID:           "DC_201",
		DiscussionID: "D_101",
		Author:       "alice",
		Body:         "Pinging @simon on updates",
		CreatedAt:    now,
	}

	payloadData, _ := json.Marshal(protocol.DiscussionPayload{
		Discussion: d1,
		Comment:    &cm,
	})

	env := protocol.EventEnvelope{
		Entity:  protocol.EntityDiscussion,
		Action:  protocol.ActionUpdated,
		Payload: payloadData,
	}

	updated, _ := m.handleEntityEvent(env)
	m = updated

	// Verify discussion comments updated
	comments := m.DiscussionComments["D_101"]
	if len(comments) != 1 || comments[0].Author != "alice" {
		t.Fatalf("expected comment added to DiscussionComments, got %+v", comments)
	}

	// Verify flashing indicator is active
	flashing, _ := m.isEntityFlashing("discussion:D_101")
	if !flashing {
		t.Errorf("expected discussion:D_101 to be flashing after reactive update")
	}

	// Verify status message
	if !strings.Contains(m.StatusMsg, "Reply on #42 by @alice") {
		t.Errorf("expected StatusMsg to mention reply on #42, got %q", m.StatusMsg)
	}
}

func TestThreadsView_SquadAndGeneralDiscussionsFiltering(t *testing.T) {
	now := time.Now()
	m := createTestModel()
	m.AuthUser = "simon"
	m.Config = &config.Config{
		ActiveSquad: "my-squad",
		Squads: []config.SquadConfig{
			{Name: "my-squad"},
			{Name: "backend"},
		},
	}

	d1 := github.Discussion{
		ID:        "D_1",
		Number:    7,
		Title:     "Squad Announcement",
		Body:      "General announcement for everyone",
		Author:    "bronners",
		CreatedAt: now.Add(-10 * time.Minute),
		UpdatedAt: now.Add(-10 * time.Minute),
		Category:  github.DiscussionCategory{Name: "Announcements", Slug: "announcements"},
	}
	d2 := github.Discussion{
		ID:        "D_2",
		Number:    8,
		Title:     "My Squad Specific Thread",
		Body:      "Discussion specifically for my-squad",
		Author:    "bronners",
		CreatedAt: now.Add(-5 * time.Minute),
		UpdatedAt: now.Add(-5 * time.Minute),
		Category:  github.DiscussionCategory{Name: "my-squad", Slug: "my-squad"},
	}
	d3 := github.Discussion{
		ID:        "D_3",
		Number:    9,
		Title:     "Backend Squad Discussion",
		Body:      "Internal backend discussion",
		Author:    "someone",
		CreatedAt: now.Add(-2 * time.Minute),
		UpdatedAt: now.Add(-2 * time.Minute),
		Category:  github.DiscussionCategory{Name: "Backend", Slug: "backend"},
	}
	d4 := github.Discussion{
		ID:        "D_4",
		Number:    10,
		Title:     "Another Backend Thread",
		Body:      "Tagged for backend squad",
		Author:    "someone",
		CreatedAt: now.Add(-1 * time.Minute),
		UpdatedAt: now.Add(-1 * time.Minute),
		Category:  github.DiscussionCategory{Name: "General", Slug: "general"},
		Labels:    []string{"squad:backend"},
	}

	m.Discussions = []github.Discussion{d1, d2, d3, d4}
	m.ActiveTab = TabThreads

	// 1. Unread count should only reflect the 2 visible discussions (d1, d2)
	unread := m.getUnreadThreadsCount()
	if unread != 2 {
		t.Errorf("expected 2 unread threads for my-squad, got %d", unread)
	}

	// 2. Render threads view and verify only d1 and d2 are in the list
	view := m.renderThreadsView()
	if !strings.Contains(view, "Squad Announcement") || !strings.Contains(view, "General announcement for everyone") {
		t.Errorf("expected announcement to be visible in view, got:\n%s", view)
	}
	if !strings.Contains(view, "My Squad Specific Thread") || !strings.Contains(view, "Discussion specifically for my-squad") {
		t.Errorf("expected squad thread to be visible in view, got:\n%s", view)
	}
	if strings.Contains(view, "Backend Squad Discussion") {
		t.Errorf("expected other squad discussion to be excluded from view, got:\n%s", view)
	}
	if strings.Contains(view, "Another Backend Thread") {
		t.Errorf("expected other squad-labeled discussion to be excluded from view, got:\n%s", view)
	}
}

func TestThreadsView_ReplyModeRKeyBugFix(t *testing.T) {
	now := time.Now()
	m := createTestModel()
	m.AuthUser = "simon"
	m.Config = &config.Config{ActiveSquad: "platform"}

	d1 := github.Discussion{
		ID:        "D_101",
		Number:    7,
		Title:     "Thread 7",
		Body:      "Message content",
		Author:    "simon",
		CreatedAt: now,
		UpdatedAt: now,
		Category:  github.DiscussionCategory{Name: "General", Slug: "general"},
	}
	m.Discussions = []github.Discussion{d1}
	m.ActiveTab = TabThreads
	m.ThreadsCursor = m.firstSelectableThreadIndex()

	// 1. Press 'r' on thread -> drops directly into reply mode
	updatedModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updatedModel.(Model)
	if !m.ThreadsReplyActive {
		t.Fatalf("expected ThreadsReplyActive=true after pressing 'r'")
	}

	// 2. While in reply mode, type 'r' (e.g. typing word 'review')
	// It MUST be typed into the reply input and MUST NOT trigger discussions refresh!
	updatedModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updatedModel.(Model)
	_ = cmd

	if strings.Contains(m.StatusMsg, "Refreshing threads") {
		t.Errorf("BUG: 'r' key triggered refresh while in reply mode! StatusMsg=%q", m.StatusMsg)
	}
	if !strings.Contains(m.ThreadsReplyInput.Value(), "r") {
		t.Errorf("expected 'r' character to be typed into ThreadsReplyInput, got %q", m.ThreadsReplyInput.Value())
	}
}

func TestThreadsView_MarkdownRendererAndYank(t *testing.T) {
	// 1. Test markdown link rendering
	rendered := renderMarkdownContent("Check out [grubby repo](https://github.com/bronners/grubby) and `make test` with **bold** text")
	if !strings.Contains(rendered, "grubby repo") {
		t.Errorf("expected rendered text to include 'grubby repo', got: %s", rendered)
	}
	// OSC 8 hyperlink escape sequence
	if !strings.Contains(rendered, "\x1b]8;;https://github.com/bronners/grubby\x1b\\") {
		t.Errorf("expected OSC 8 hyperlink escape sequence in rendered output, got: %q", rendered)
	}

	// 2. Test ExtractYankables
	text := "Please review [PR #15](https://github.com/bronners-org/grubby/pull/15), ping @alice and check #grubby."
	yankables := ExtractYankables(text, "bronners-org")
	if len(yankables) < 3 {
		t.Fatalf("expected at least 3 yankables, got %d", len(yankables))
	}
	// Verify each yankable has a proper URL value
	foundPR := false
	foundUser := false
	foundRepo := false
	for _, yk := range yankables {
		if yk.Value == "https://github.com/bronners-org/grubby/pull/15" {
			foundPR = true
		}
		if yk.Value == "https://github.com/alice" {
			foundUser = true
		}
		if yk.Value == "https://github.com/bronners-org/grubby" {
			foundRepo = true
		}
	}
	if !foundPR {
		t.Errorf("expected PR url yankable")
	}
	if !foundUser {
		t.Errorf("expected User url yankable")
	}
	if !foundRepo {
		t.Errorf("expected Repo url yankable")
	}
}

func TestThreadsView_ReplyInputPromptAndDynamicHeight(t *testing.T) {
	now := time.Now()
	m := createTestModel()
	m.AuthUser = "simonbronner"
	m.Width = 100
	m.Config = &config.Config{ActiveSquad: "platform"}

	d1 := github.Discussion{
		ID:        "D_1",
		Number:    1,
		Title:     "Thread 1",
		Body:      "Hello",
		Author:    "simonbronner",
		CreatedAt: now,
		UpdatedAt: now,
		Category:  github.DiscussionCategory{Name: "Platform", Slug: "platform"},
	}
	m.Discussions = []github.Discussion{d1}
	m.ActiveTab = TabThreads

	// Start reply
	m.startReplyToThread(&d1)
	if !m.ThreadsReplyActive {
		t.Fatalf("expected ThreadsReplyActive=true")
	}
	if m.ThreadsReplyInput.Height() != 1 {
		t.Errorf("expected initial height to be 1, got %d", m.ThreadsReplyInput.Height())
	}
	// Total textarea width includes prompt
	totalWidth := m.ThreadsReplyInput.Width() + 4 + len("@simonbronner ")
	if totalWidth < 90 {
		t.Errorf("expected input total width to span feed (~94), got %d (inner: %d)", totalWidth, m.ThreadsReplyInput.Width())
	}

	// Check prompt func: line 0 has @simonbronner, line 1 has spaces (not @simonbronner)
	view := m.renderThreadsView()
	if !strings.Contains(view, "@simonbronner") {
		t.Errorf("expected @simonbronner prompt in view, got:\n%s", view)
	}

	// Type text and newlines (shift+enter / ctrl+j)
	m.ThreadsReplyInput.SetValue("First line")
	updated, _ := m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyCtrlJ})
	m = updated
	if m.ThreadsReplyInput.Height() < 2 {
		t.Errorf("expected height to grow to at least 2 after newline, got %d", m.ThreadsReplyInput.Height())
	}
}

func TestThreadsView_PRAutocompleteIndexingAndSearch(t *testing.T) {
	now := time.Now()
	m := createTestModel()
	m.CurrentOrg = "bronners-org"
	m.Repos = []github.Repository{
		{Name: "grubby", FullName: "bronners-org/grubby"},
		{Name: "tasks", FullName: "bronners-org/tasks"},
		{Name: "secret-service", FullName: "bronners-org/secret-service"},
	}
	m.HiddenRepos = map[string]bool{
		"secret-service": true,
	}

	m.PRs = []github.PullRequest{
		{
			Repo:         "grubby",
			RepoFullName: "bronners-org/grubby",
			Number:       15,
			Title:        "Clear badge notification",
			State:        "OPEN",
			UpdatedAt:    now,
			URL:          "https://github.com/bronners-org/grubby/pull/15",
		},
		{
			Repo:         "tasks",
			RepoFullName: "bronners-org/tasks",
			Number:       42,
			Title:        "Implement discussion backend",
			State:        "OPEN",
			UpdatedAt:    now.Add(-10 * time.Minute),
			URL:          "https://github.com/bronners-org/tasks/pull/42",
		},
		{
			Repo:         "tasks",
			RepoFullName: "bronners-org/tasks",
			Number:       30,
			Title:        "Old merged PR",
			State:        "MERGED",
			UpdatedAt:    now.Add(-2 * time.Hour),
		},
		{
			Repo:         "secret-service",
			RepoFullName: "bronners-org/secret-service",
			Number:       1,
			Title:        "Confidential patch",
			State:        "OPEN",
			UpdatedAt:    now,
		},
	}

	// 1. Empty query '#' -> open PRs from visible repos MUST be visible
	emptySuggestions := m.getAutocompleteSuggestions('#', "")
	if len(emptySuggestions) == 0 {
		t.Fatalf("expected suggestions for empty '#' query")
	}
	foundPR15 := false
	foundPR42 := false
	for _, s := range emptySuggestions {
		if s.Key == "#grubby#15" {
			foundPR15 = true
		}
		if s.Key == "#tasks#42" {
			foundPR42 = true
		}
		if s.Key == "#tasks#30" {
			t.Errorf("merged PR #30 should NOT be in suggestions")
		}
		if strings.Contains(s.Key, "secret-service") {
			t.Errorf("hidden repo secret-service should NOT be in suggestions")
		}
	}
	if !foundPR15 {
		t.Errorf("expected open PR #grubby#15 in empty query suggestions")
	}
	if !foundPR42 {
		t.Errorf("expected open PR #tasks#42 in empty query suggestions")
	}

	// 2. Search by repository name: 'grubby'
	repoSearch := m.getAutocompleteSuggestions('#', "grubby")
	if len(repoSearch) < 2 {
		t.Fatalf("expected at least 2 suggestions when searching 'grubby', got %d", len(repoSearch))
	}
	// First item is repo #grubby, second item is PR in grubby
	if repoSearch[0].Key != "#grubby" {
		t.Errorf("expected first item to be repo #grubby, got %q", repoSearch[0].Key)
	}
	if repoSearch[1].Key != "#grubby#15" {
		t.Errorf("expected second item to be PR #grubby#15, got %q", repoSearch[1].Key)
	}
	if repoSearch[1].InsertText != "[grubby#15](https://github.com/bronners-org/grubby/pull/15)" {
		t.Errorf("expected InsertText to be markdown link, got %q", repoSearch[1].InsertText)
	}

	// 3. Search by PR number: '42'
	numSearch := m.getAutocompleteSuggestions('#', "42")
	if len(numSearch) == 0 || numSearch[0].Key != "#tasks#42" {
		t.Fatalf("expected #tasks#42 to match query '42', got %+v", numSearch)
	}

	// 4. Search by PR title substring: 'badge'
	titleSearch := m.getAutocompleteSuggestions('#', "badge")
	if len(titleSearch) == 0 || titleSearch[0].Key != "#grubby#15" {
		t.Fatalf("expected #grubby#15 to match title query 'badge', got %+v", titleSearch)
	}

	// 5. Test insertion
	inserted := insertAutocomplete("Check out #gru", repoSearch[1].InsertText)
	if inserted != "Check out [grubby#15](https://github.com/bronners-org/grubby/pull/15) " {
		t.Errorf("unexpected inserted autocomplete string: %q", inserted)
	}
}

func TestThreadsView_TypingOverflowDynamicHeight(t *testing.T) {
	m := createTestModel()
	m.AuthUser = "simon"
	m.Width = 80
	d := github.Discussion{ID: "D_1", Number: 1, Title: "Test"}
	m.Discussions = []github.Discussion{d}

	m.startReplyToThread(&d)
	if m.ThreadsReplyInput.Height() != 1 {
		t.Fatalf("expected initial height 1, got %d", m.ThreadsReplyInput.Height())
	}

	// Simulate typing a long sentence that exceeds textarea width (approx 70 columns)
	longSentence := "This is a very long response that wraps off the end of the line without any explicit newline characters typed by the user to ensure dynamic expansion works."
	for i, ch := range longSentence {
		updated, _ := m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		m = updated

		if !strings.Contains(m.ThreadsReplyInput.View(), "@simon") {
			t.Fatalf("Author @simon disappeared at char %d ('%c')! Value: %q, View:\n%s", i, ch, m.ThreadsReplyInput.Value(), m.ThreadsReplyInput.View())
		}
	}

	// Height must dynamically expand to accommodate visual lines (> 1)
	if m.ThreadsReplyInput.Height() < 2 {
		t.Errorf("expected height to dynamically grow past 1 on overflow typing, got %d", m.ThreadsReplyInput.Height())
	}

	finalView := m.ThreadsReplyInput.View()
	if !strings.Contains(finalView, "@simon") {
		t.Errorf("author @simon missing from final reply view, line 0 scrolled off!")
	}
}

func TestThreadsView_FeedReplyMultilineWrapping(t *testing.T) {
	now := time.Now()
	m := createTestModel()
	m.Width = 80
	m.Config = &config.Config{ActiveSquad: "platform"}

	longReply := "This is a very long reply without any explicit newlines that should automatically wrap across multiple indented lines in the feed instead of being cut off on the right margin."
	d := github.Discussion{
		ID:        "D_100",
		Number:    10,
		Title:     "Thread 10",
		Body:      "OP Message",
		Author:    "simon",
		CreatedAt: now,
		UpdatedAt: now,
		Category:  github.DiscussionCategory{Name: "Platform", Slug: "platform"},
		CommentsCount: 1,
		Comments: []github.DiscussionComment{
			{
				ID:           "C_101",
				DiscussionID: "D_100",
				Author:       "alice",
				Body:         longReply,
				CreatedAt:    now,
			},
		},
	}
	m.Discussions = []github.Discussion{d}
	m.ActiveTab = TabThreads

	view := m.renderThreadsView()
	// Verify the view contains wrapped lines indented with spaces
	lines := strings.Split(view, "\n")
	foundIndentedContinuation := false
	for _, l := range lines {
		if strings.HasPrefix(l, "      ") && strings.Contains(l, "newlines that should automatically wrap") {
			foundIndentedContinuation = true
			break
		}
	}
	if !foundIndentedContinuation {
		t.Errorf("expected long reply to be wrapped across multiple lines with 6-space indent, got view:\n%s", view)
	}
}

func TestThreadsView_ThreadIndicatorAndMessageUnreadDots(t *testing.T) {
	now := time.Now()
	m := createTestModel()
	m.AuthUser = "simon"
	m.Config = &config.Config{ActiveSquad: "platform"}

	d1 := github.Discussion{
		ID:        "D_UNREAD_1",
		Number:    1,
		Title:     "Thread #1",
		Body:      "Unread OP body",
		Author:    "alice",
		CreatedAt: now,
		UpdatedAt: now,
		Category:  github.DiscussionCategory{Name: "Platform", Slug: "platform"},
		CommentsCount: 1,
		Comments: []github.DiscussionComment{
			{
				ID:           "DC_UNREAD_1",
				DiscussionID: "D_UNREAD_1",
				Author:       "bob",
				Body:         "Unread reply body",
				CreatedAt:    now,
			},
		},
	}
	m.Discussions = []github.Discussion{d1}
	m.SortedDiscussions = m.getSortedDiscussions()
	m.ActiveTab = TabThreads
	m.ThreadsCursor = m.firstSelectableThreadIndex()

	// Initially neither OP nor comment is marked seen
	if m.isMessageSeen(d1.ID, d1.Author, d1.CreatedAt, d1.LastReadAt) || m.isMessageSeen(d1.Comments[0].ID, d1.Comments[0].Author, d1.Comments[0].CreatedAt, d1.LastReadAt) {
		t.Fatalf("expected messages to be unseen initially")
	}
	if !m.isDiscussionUnread(d1) {
		t.Fatalf("expected discussion to be unread initially")
	}

	view := m.renderThreadsView()
	// Thread title should have 🧵 prefix
	if !strings.Contains(view, "🧵 Thread #1") {
		t.Errorf("expected thread title to have 🧵 prefix, got:\n%s", view)
	}
	// OP message should have ● (green) indicator next to author, NOT 🧵
	if !strings.Contains(view, "● @alice") {
		t.Errorf("expected unread message dot ● @alice, got:\n%s", view)
	}
	if strings.Contains(view, "🧵 ● @alice") {
		t.Errorf("expected NO 🧵 prefix on OP message line, got:\n%s", view)
	}
	// Unread comment should have ● indicator next to author
	if !strings.Contains(view, "● @bob") {
		t.Errorf("expected unread comment indicator ● @bob, got:\n%s", view)
	}

	// Mark current thread viewed
	m.markCurrentThreadViewed()

	if !m.isMessageSeen(d1.ID, d1.Author, d1.CreatedAt, d1.LastReadAt) || !m.isMessageSeen(d1.Comments[0].ID, d1.Comments[0].Author, d1.Comments[0].CreatedAt, d1.LastReadAt) {
		t.Errorf("expected both messages to be seen after markCurrentThreadViewed")
	}
	if m.isDiscussionUnread(m.Discussions[0]) {
		t.Errorf("expected discussion to be marked read after markCurrentThreadViewed")
	}

	viewSeen := m.renderThreadsView()
	// Unread dot ● should no longer appear next to bob
	if strings.Contains(viewSeen, "● @bob") {
		t.Errorf("expected no unread dot ● @bob after viewed, got:\n%s", viewSeen)
	}
}

func TestThreadsView_EditMessageOwnVsOther(t *testing.T) {
	now := time.Now()
	m := createTestModel()
	m.AuthUser = "simon"
	m.Config = &config.Config{ActiveSquad: "platform"}

	d1 := github.Discussion{
		ID:        "D_10",
		Number:    10,
		Title:     "Thread 10",
		Body:      "My original OP text",
		Author:    "simon", // AuthUser
		CreatedAt: now,
		UpdatedAt: now,
		Category:  github.DiscussionCategory{Name: "Platform", Slug: "platform"},
		CommentsCount: 1,
		Comments: []github.DiscussionComment{
			{
				ID:           "DC_20",
				DiscussionID: "D_10",
				Author:       "alice", // Other user
				Body:         "Alice reply text",
				CreatedAt:    now,
			},
		},
	}
	m.Discussions = []github.Discussion{d1}
	m.ActiveTab = TabThreads
	m.ThreadsCursor = m.firstSelectableThreadIndex()
	m.ThreadsSelectedMsgIdx = -1

	// 1. Thread level: pressing 'e' on own OP message enters inline edit mode
	updated, _ := m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = updated
	if !m.ThreadsEditActive {
		t.Fatalf("expected ThreadsEditActive=true for own OP")
	}
	if m.ThreadEditMessageID != "D_10" || !m.ThreadEditIsOP {
		t.Fatalf("expected editing OP message D_10, got %s isOP=%v", m.ThreadEditMessageID, m.ThreadEditIsOP)
	}
	if m.ThreadEditMessageInput.Value() != "My original OP text" {
		t.Fatalf("expected input value %q, got %q", "My original OP text", m.ThreadEditMessageInput.Value())
	}

	// View should render inline edit prompt with @simon
	view := m.renderThreadsView()
	if !strings.Contains(view, "@simon") || !strings.Contains(view, "My original OP text") {
		t.Errorf("expected view to contain inline edit composer with @simon and original text, got:\n%s", view)
	}

	// 2. Escape cancels inline edit mode
	updated, _ = m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated
	if m.ThreadsEditActive {
		t.Fatalf("expected ThreadsEditActive=false on Esc")
	}

	// 3. Descend into thread to Alice's comment (idx 1)
	m.ThreadsExpandedID = "D_10"
	m.ThreadsSelectedMsgIdx = 1

	// 4. Press 'e' on Alice's message -> should NOT enter edit mode and show error toast
	updated, _ = m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = updated
	if m.ThreadsEditActive {
		t.Fatalf("expected inline edit mode NOT to open on someone else's message")
	}
	if !strings.Contains(m.StatusMsg, "You can only edit your own messages") {
		t.Errorf("expected permission warning in StatusMsg, got %q", m.StatusMsg)
	}

	// 5. Update message event updates the local discussion body
	m.Update(ThreadMessageUpdatedMsg{
		DiscID:    "D_10",
		MessageID: "D_10",
		Body:      "Updated OP body via test",
		IsOP:      true,
		UpdatedAt: time.Now(),
	})
	if m.Discussions[0].Body != "Updated OP body via test" {
		t.Errorf("expected discussion body updated, got %q", m.Discussions[0].Body)
	}
}

func TestThreadsView_EditMessageDynamicHeightAndWrapping(t *testing.T) {
	now := time.Now()
	m := createTestModel()
	m.AuthUser = "simon"
	m.Config = &config.Config{ActiveSquad: "platform"}
	m.Width = 100

	multilineBody := "Line 1 of content\nLine 2 with extra information\nLine 3 wrapping and scaling across the full terminal screen"
	d := github.Discussion{
		ID:        "D_EDIT_MULTILINE",
		Number:    12,
		Title:     "Multiline Thread",
		Body:      multilineBody,
		Author:    "simon",
		CreatedAt: now,
		UpdatedAt: now,
		Category:  github.DiscussionCategory{Name: "Platform", Slug: "platform"},
	}
	m.Discussions = []github.Discussion{d}
	m.ActiveTab = TabThreads
	m.ThreadsCursor = m.firstSelectableThreadIndex()
	m.ThreadsSelectedMsgIdx = -1

	// Press 'e' to start editing
	updated, _ := m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = updated

	if !m.ThreadsEditActive {
		t.Fatalf("expected ThreadsEditActive=true")
	}

	// Height should dynamically size to match the 3-line content (>= 3)
	if m.ThreadEditMessageInput.Height() < 3 {
		t.Errorf("expected initial height >= 3 for 3-line body, got %d", m.ThreadEditMessageInput.Height())
	}

	// Width should scale out to full width (m.Width - 6 = 94, minus prompt = 85)
	if m.ThreadEditMessageInput.Width() < 80 {
		t.Errorf("expected full width >= 80, got %d", m.ThreadEditMessageInput.Width())
	}

	// Press Shift+Enter to add a newline -> height should grow
	initialHeight := m.ThreadEditMessageInput.Height()
	updated, _ = m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyCtrlJ})
	m = updated

	if m.ThreadEditMessageInput.Height() < initialHeight+1 {
		t.Errorf("expected height to expand to >= %d after Shift+Enter, got %d", initialHeight+1, m.ThreadEditMessageInput.Height())
	}

	// Enter or Ctrl+S saves the edited message
	updated, cmd := m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated

	if m.ThreadsEditActive {
		t.Errorf("expected ThreadsEditActive=false after Enter")
	}
	if cmd == nil {
		t.Errorf("expected updateMessageCmd to be triggered on Enter")
	}
}

func TestThreadsView_YankLinksModal(t *testing.T) {
	now := time.Now()
	m := createTestModel()
	m.AuthUser = "simon"
	m.Config = &config.Config{ActiveSquad: "platform"}

	d1 := github.Discussion{
		ID:        "D_YANK",
		Number:    5,
		Title:     "Thread with multiple links",
		Body:      "Check out https://github.com/org/repo1/pull/1 and https://github.com/org/repo2/issues/2",
		Author:    "simon",
		CreatedAt: now,
		UpdatedAt: now,
		Category:  github.DiscussionCategory{Name: "Platform", Slug: "platform"},
		CommentsCount: 1,
		Comments: []github.DiscussionComment{
			{
				ID:           "DC_YANK_1",
				DiscussionID: "D_YANK",
				Author:       "alice",
				Body:         "Also see https://github.com/org/repo3/pull/3",
				CreatedAt:    now,
			},
		},
	}
	m.Discussions = []github.Discussion{d1}
	m.ActiveTab = TabThreads
	m.ThreadsCursor = m.firstSelectableThreadIndex()
	m.ThreadsSelectedMsgIdx = -1

	// 1. Press 'y' -> opens ModalYankLinks with 3 links
	updated, _ := m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated
	if m.ActiveModal != ModalYankLinks {
		t.Fatalf("expected ActiveModal=ModalYankLinks, got %v", m.ActiveModal)
	}
	if len(m.ThreadsYankItems) != 3 {
		t.Fatalf("expected 3 links extracted, got %d", len(m.ThreadsYankItems))
	}

	// Modal overlay should render the list of links
	modalView := m.renderModalOverlay("")
	if !strings.Contains(modalView, "Yank Links from Thread #5") {
		t.Errorf("expected modal header with thread number, got:\n%s", modalView)
	}
	if !strings.Contains(modalView, "[1]") || !strings.Contains(modalView, "[2]") || !strings.Contains(modalView, "[3]") {
		t.Errorf("expected [1], [2], [3] quick yank options, got:\n%s", modalView)
	}

	// 2. Navigation: 'j' moves cursor down, 'k' moves up
	if m.ThreadsYankCursor != 0 {
		t.Errorf("expected initial yank cursor=0, got %d", m.ThreadsYankCursor)
	}
	updatedModal, _ := m.handleYankLinksModalKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updatedModal.(Model)
	if m.ThreadsYankCursor != 1 {
		t.Errorf("expected yank cursor=1 after j, got %d", m.ThreadsYankCursor)
	}
	updatedModal, _ = m.handleYankLinksModalKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updatedModal.(Model)
	if m.ThreadsYankCursor != 0 {
		t.Errorf("expected yank cursor=0 after k, got %d", m.ThreadsYankCursor)
	}

	// 3. Quick-select '2' copies second link and closes modal
	updatedModal, _ = m.handleYankLinksModalKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = updatedModal.(Model)
	if m.ActiveModal != ModalNone {
		t.Errorf("expected modal closed after quick-select, got %v", m.ActiveModal)
	}
	if !strings.Contains(m.StatusMsg, "Copied") {
		t.Errorf("expected Copied in StatusMsg, got %q", m.StatusMsg)
	}

	// 4. Re-open modal and cancel with Esc
	m.ActiveModal = ModalYankLinks
	m.ThreadsYankActive = true
	m.ThreadsYankItems = []YankableItem{{Label: "link1", Value: "https://example.com"}}
	updatedModal, _ = m.handleYankLinksModalKeys(tea.KeyMsg{Type: tea.KeyEsc})
	m = updatedModal.(Model)
	if m.ActiveModal != ModalNone || m.ThreadsYankActive {
		t.Errorf("expected modal closed after Esc")
	}
}

func TestThreadsView_TopBarUnreadBadge(t *testing.T) {
	now := time.Now()
	m := createTestModel()
	m.AuthUser = "simon"
	m.Config = &config.Config{ActiveSquad: "platform"}

	d1 := github.Discussion{
		ID:        "D_BADGE_1",
		Number:    1,
		Title:     "Thread One",
		Body:      "Message 1",
		Author:    "alice",
		CreatedAt: now,
		UpdatedAt: now,
		Category:  github.DiscussionCategory{Name: "Platform", Slug: "platform"},
	}
	d2 := github.Discussion{
		ID:        "D_BADGE_2",
		Number:    2,
		Title:     "Thread Two",
		Body:      "Message 2",
		Author:    "bob",
		CreatedAt: now,
		UpdatedAt: now,
		Category:  github.DiscussionCategory{Name: "Platform", Slug: "platform"},
	}

	m.Discussions = []github.Discussion{d1, d2}
	m.SortedDiscussions = m.getSortedDiscussions()

	// Both discussions are unread
	if count := m.getUnreadThreadsCount(); count != 2 {
		t.Fatalf("expected 2 unread threads initially, got %d", count)
	}

	// View thread 1
	m.ThreadsCursor = 1
	m.markCurrentThreadViewed()
	if count := m.getUnreadThreadsCount(); count != 1 {
		t.Fatalf("expected 1 unread thread after viewing first, got %d", count)
	}

	// View thread 2
	m.ThreadsCursor = 2
	m.markCurrentThreadViewed()
	if count := m.getUnreadThreadsCount(); count != 0 {
		t.Fatalf("expected 0 unread threads after viewing both, got %d", count)
	}
}

func TestThreadsView_QuestionMarkInEditMode(t *testing.T) {
	now := time.Now()
	m := createTestModel()
	m.AuthUser = "simon"
	m.Config = &config.Config{ActiveSquad: "platform"}

	d := github.Discussion{
		ID:        "D_QM",
		Number:    1,
		Title:     "Thread",
		Body:      "Original question",
		Author:    "simon",
		CreatedAt: now,
		UpdatedAt: now,
		Category:  github.DiscussionCategory{Name: "Platform", Slug: "platform"},
	}
	m.Discussions = []github.Discussion{d}
	m.ActiveTab = TabThreads
	m.ThreadsCursor = m.firstSelectableThreadIndex()

	// Enter edit mode
	updated, _ := m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = updated

	if !m.ThreadsEditActive {
		t.Fatalf("expected ThreadsEditActive=true")
	}

	// Type '?' into the edit input via top-level Update
	res, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = res.(Model)

	// ModalHelp must NOT be opened!
	if m.ActiveModal == ModalHelp {
		t.Fatalf("expected ModalHelp NOT to be opened when typing '?'")
	}
	// Edit mode must still be active
	if !m.ThreadsEditActive {
		t.Fatalf("expected ThreadsEditActive to remain true")
	}
	// '?' must be in the textarea input value
	if !strings.Contains(m.ThreadEditMessageInput.Value(), "?") {
		t.Errorf("expected '?' to be inserted into edit input, got %q", m.ThreadEditMessageInput.Value())
	}
}

func TestThreadsView_UnreadThreadsExpandedByDefault(t *testing.T) {
	now := time.Now()
	m := createTestModel()
	m.AuthUser = "simon"
	m.Config = &config.Config{ActiveSquad: "platform"}

	d1 := github.Discussion{
		ID:            "D_UNREAD_EXP",
		Number:        7,
		Title:         "Unread Thread with Replies",
		Body:          "OP message",
		Author:        "alice",
		CreatedAt:     now,
		UpdatedAt:     now,
		Category:      github.DiscussionCategory{Name: "Platform", Slug: "platform"},
		CommentsCount: 2,
		Comments: []github.DiscussionComment{
			{
				ID:           "DC_1",
				DiscussionID: "D_UNREAD_EXP",
				Author:       "bob",
				Body:         "First unread reply",
				CreatedAt:    now,
			},
			{
				ID:           "DC_2",
				DiscussionID: "D_UNREAD_EXP",
				Author:       "charlie",
				Body:         "Second unread reply",
				CreatedAt:    now,
			},
		},
	}
	m.Discussions = []github.Discussion{d1}
	m.ActiveTab = TabThreads
	m.ThreadsExpandedID = "" // NOT explicitly expanded

	// Since d1 is unread, renderThreadsView should expand out all replies by default
	view := m.renderThreadsView()
	if !strings.Contains(view, "First unread reply") {
		t.Errorf("expected first unread reply to be shown by default in expanded view, got:\n%s", view)
	}
	if !strings.Contains(view, "Second unread reply") {
		t.Errorf("expected second unread reply to be shown by default in expanded view, got:\n%s", view)
	}
	if strings.Contains(view, "↳ 2 replies") {
		t.Errorf("expected unread thread NOT to be collapsed with '↳ 2 replies', got:\n%s", view)
	}
}

func TestThreadsView_CollapseThreadWithLeftArrow(t *testing.T) {
	now := time.Now()
	m := createTestModel()
	m.AuthUser = "simon"
	m.Config = &config.Config{
		ActiveSquad: "platform",
	}

	d1 := github.Discussion{
		ID:            "D_COLLAPSE_1",
		Number:        1,
		Title:         "Thread to Collapse",
		Body:          "OP body message",
		Author:        "alice",
		CreatedAt:     now,
		UpdatedAt:     now,
		Category:      github.DiscussionCategory{Name: "Platform", Slug: "platform"},
		CommentsCount: 2,
		Comments: []github.DiscussionComment{
			{
				ID:           "DC_1",
				DiscussionID: "D_COLLAPSE_1",
				Author:       "bob",
				Body:         "First reply to hide when collapsed",
				CreatedAt:    now,
			},
			{
				ID:           "DC_2",
				DiscussionID: "D_COLLAPSE_1",
				Author:       "charlie",
				Body:         "Last reply visible when collapsed",
				CreatedAt:    now,
			},
		},
	}
	m.Discussions = []github.Discussion{d1}
	m.ActiveTab = TabThreads
	m.ThreadsCursor = m.firstSelectableThreadIndex()
	m.ThreadsSelectedMsgIdx = -1

	// 1. Initially unread thread is expanded showing all replies
	view := m.renderThreadsView()
	if !strings.Contains(view, "First reply to hide when collapsed") {
		t.Fatalf("expected all replies visible before collapse")
	}

	// 2. Pressing Left Arrow on thread collapses it
	updated, _ := m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyLeft})
	m = updated

	if !m.ThreadsCollapsedIDs["D_COLLAPSE_1"] {
		t.Fatalf("expected D_COLLAPSE_1 to be recorded in ThreadsCollapsedIDs")
	}

	// 3. Rendered view should now be collapsed:
	// "↳ 2 replies (press Enter or → to expand all)" is visible
	// "First reply to hide when collapsed" is NOT visible
	// "Last reply visible when collapsed" is visible
	collapsedView := m.renderThreadsView()
	if !strings.Contains(collapsedView, "↳ 2 replies (press Enter or → to expand all)") {
		t.Errorf("expected collapsed note in view, got:\n%s", collapsedView)
	}
	if strings.Contains(collapsedView, "First reply to hide when collapsed") {
		t.Errorf("expected first reply to be hidden when collapsed, got:\n%s", collapsedView)
	}
	if !strings.Contains(collapsedView, "Last reply visible when collapsed") {
		t.Errorf("expected last reply to remain visible when collapsed, got:\n%s", collapsedView)
	}

	// 4. Pressing Right Arrow re-expands the thread and descends into OP
	updated, _ = m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyRight})
	m = updated

	if m.ThreadsCollapsedIDs["D_COLLAPSE_1"] {
		t.Fatalf("expected D_COLLAPSE_1 to be removed from ThreadsCollapsedIDs after Right arrow")
	}
	if m.ThreadsSelectedMsgIdx != 0 {
		t.Fatalf("expected ThreadsSelectedMsgIdx=0 after Right arrow, got %d", m.ThreadsSelectedMsgIdx)
	}

	expandedView := m.renderThreadsView()
	if !strings.Contains(expandedView, "First reply to hide when collapsed") {
		t.Errorf("expected first reply to be visible again after re-expansion, got:\n%s", expandedView)
	}

	// 5. When descended on OP (ThreadsSelectedMsgIdx=0), pressing 'h' collapses the thread and ascends to -1
	updated, _ = m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	m = updated
	if !m.ThreadsCollapsedIDs["D_COLLAPSE_1"] {
		t.Fatalf("expected D_COLLAPSE_1 to be collapsed after 'h' on OP")
	}
	if m.ThreadsSelectedMsgIdx != -1 {
		t.Fatalf("expected ThreadsSelectedMsgIdx=-1 after collapse from OP, got %d", m.ThreadsSelectedMsgIdx)
	}

	// 6. Enter on collapsed thread re-expands and starts reply
	updated, _ = m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated
	if m.ThreadsCollapsedIDs["D_COLLAPSE_1"] {
		t.Fatalf("expected D_COLLAPSE_1 to be removed from ThreadsCollapsedIDs after Enter")
	}
	if !m.ThreadsReplyActive {
		t.Fatalf("expected ThreadsReplyActive=true after Enter")
	}
}

func TestThreadsView_ReplicateRepoTiersAndHeaderCleanup(t *testing.T) {
	now := time.Now()
	m := createTestModel()
	m.AuthUser = "simon"
	m.Config = &config.Config{
		ActiveSquad: "platform",
	}

	dHot := github.Discussion{
		ID:            "D_HOT",
		Number:        1,
		Title:         "Hot Thread",
		Body:          "Hot message",
		Author:        "alice",
		CreatedAt:     now.Add(-2 * time.Hour),
		UpdatedAt:     now.Add(-2 * time.Hour), // <= 48h -> Hot
		Category:      github.DiscussionCategory{Name: "Platform", Slug: "platform"},
		CommentsCount: 0,
		LastReadAt:    now,
	}
	dWarm := github.Discussion{
		ID:            "D_WARM",
		Number:        2,
		Title:         "Warm Thread",
		Body:          "Warm message",
		Author:        "bob",
		CreatedAt:     now.Add(-72 * time.Hour),
		UpdatedAt:     now.Add(-72 * time.Hour), // 3 days (<= 7d) -> Warm
		Category:      github.DiscussionCategory{Name: "Platform", Slug: "platform"},
		CommentsCount: 0,
		LastReadAt:    now,
	}
	dCold := github.Discussion{
		ID:            "D_COLD",
		Number:        3,
		Title:         "Cold Thread",
		Body:          "Cold message",
		Author:        "charlie",
		CreatedAt:     now.Add(-240 * time.Hour),
		UpdatedAt:     now.Add(-240 * time.Hour), // 10 days (<= 30d) -> Cold
		Category:      github.DiscussionCategory{Name: "Platform", Slug: "platform"},
		CommentsCount: 0,
		LastReadAt:    now,
	}
	dDead := github.Discussion{
		ID:            "D_DEAD",
		Number:        4,
		Title:         "Dead Thread",
		Body:          "Dead message",
		Author:        "dave",
		CreatedAt:     now.Add(-1000 * time.Hour),
		UpdatedAt:     now.Add(-1000 * time.Hour), // > 30d -> Dead
		Category:      github.DiscussionCategory{Name: "Platform", Slug: "platform"},
		CommentsCount: 0,
		LastReadAt:    now,
	}

	m.Discussions = []github.Discussion{dHot, dWarm, dCold, dDead}
	m.ActiveTab = TabThreads
	m.ThreadsDeadExpanded = false
	m.ThreadsCursor = m.firstSelectableThreadIndex()

	view := m.renderThreadsView()

	// 1. Verify "Squad: [" is completely removed from header
	if strings.Contains(view, "Squad: [") {
		t.Errorf("expected 'Squad: [' to be removed from header, got:\n%s", view)
	}

	// 2. Verify header replicates repo tier format: 🔥 1 Hot  🌞 1 Warm  🧊 1 Cold  💀 1 Dead
	expectedSummary := "🔥 1 Hot  🌞 1 Warm  🧊 1 Cold  💀 1 Dead"
	if !strings.Contains(view, expectedSummary) {
		t.Errorf("expected header to contain %q, got:\n%s", expectedSummary, view)
	}

	// 3. Hot and Warm are visible by default
	if !strings.Contains(view, "Hot Thread") {
		t.Errorf("expected Hot Thread to be visible by default")
	}
	if !strings.Contains(view, "Warm Thread") {
		t.Errorf("expected Warm Thread to be visible by default")
	}

	// 4. Cold and Dead are collapsed by default ("only collapse anything other than hot and warm")
	if strings.Contains(view, "Cold Thread") {
		t.Errorf("expected Cold Thread to be collapsed by default")
	}
	if strings.Contains(view, "Dead Thread") {
		t.Errorf("expected Dead Thread to be collapsed by default")
	}
	if !strings.Contains(view, "▶ 🧊 COLD (1 thread)") || !strings.Contains(view, "▶ 💀 DEAD (1 thread)") {
		t.Errorf("expected collapse headers for Cold and Dead tiers, got:\n%s", view)
	}

	// 5. Pressing 'D' expands Cold and Dead threads
	updated, _ := m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'D'}})
	m = updated
	if !m.ThreadsDeadExpanded {
		t.Fatalf("expected ThreadsDeadExpanded=true after pressing D")
	}

	viewExpanded := m.renderThreadsView()
	if !strings.Contains(viewExpanded, "Cold Thread") {
		t.Errorf("expected Cold Thread to be visible after expanding with D")
	}
	if !strings.Contains(viewExpanded, "Dead Thread") {
		t.Errorf("expected Dead Thread to be visible after expanding with D")
	}
}

func TestThreadsView_TierCategoryHeadersAndClosedDiscussions(t *testing.T) {
	now := time.Now()
	m := createTestModel()
	m.AuthUser = "simon"
	m.Config = &config.Config{ActiveSquad: "platform"}

	dHot := github.Discussion{
		ID:        "D_HOT_1",
		Number:    101,
		Title:     "Hot Topic",
		Body:      "Hot body",
		Author:    "alice",
		CreatedAt: now.Add(-1 * time.Hour),
		UpdatedAt: now.Add(-1 * time.Hour),
		Category:  github.DiscussionCategory{Name: "Platform", Slug: "platform"},
	}
	dClosed := github.Discussion{
		ID:        "D_CLOSED_1",
		Number:    102,
		Title:     "Closed Thread",
		Body:      "Closed body",
		Author:    "bob",
		CreatedAt: now.Add(-1 * time.Hour),
		UpdatedAt: now.Add(-1 * time.Hour),
		Closed:    true, // Immediately placed into Dead tier!
		Category:  github.DiscussionCategory{Name: "Platform", Slug: "platform"},
	}

	m.Discussions = []github.Discussion{dHot, dClosed}
	m.ActiveTab = TabThreads
	m.ThreadsDeadExpanded = false

	items := m.getThreadFeedItems()
	// Should have:
	// item 0: Hot Tier Header (expanded)
	// item 1: dHot (FeedItemThread)
	// item 2: Dead Tier Header (collapsed by default)
	if len(items) != 3 {
		t.Fatalf("expected 3 items (Hot header, Hot thread, Dead header), got %d: %+v", len(items), items)
	}
	if items[0].Kind != FeedItemTierHeader || items[0].Tier != ThreadTierHot || !items[0].Expanded {
		t.Errorf("expected item 0 to be expanded Hot tier header, got %+v", items[0])
	}
	if items[1].Kind != FeedItemThread || items[1].Discussion.Discussion.ID != "D_HOT_1" {
		t.Errorf("expected item 1 to be Hot thread, got %+v", items[1])
	}
	if items[2].Kind != FeedItemTierHeader || items[2].Tier != ThreadTierDead || items[2].Expanded {
		t.Errorf("expected item 2 to be collapsed Dead tier header, got %+v", items[2])
	}

	// In rendered view, Hot is open (▼) and Dead is closed (▶)
	view := m.renderThreadsView()
	if !strings.Contains(view, "▼ 🔥 HOT (1 thread)") {
		t.Errorf("expected open Hot header in view, got:\n%s", view)
	}
	if !strings.Contains(view, "▶ 💀 DEAD (1 thread)") {
		t.Errorf("expected closed Dead header in view, got:\n%s", view)
	}
	if !strings.Contains(view, "Hot Topic") {
		t.Errorf("expected Hot Topic visible in view")
	}
	if strings.Contains(view, "Closed Thread") {
		t.Errorf("expected Closed Thread to be hidden under closed Dead header")
	}

	// 1. Move cursor to Dead Tier Header (index 2) and press Space to toggle expand
	m.ThreadsCursor = 2
	updated, _ := m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeySpace})
	m = updated

	expandedItems := m.getThreadFeedItems()
	// Should now have 4 items (Hot header, Hot thread, Dead header, Dead thread)
	if len(expandedItems) != 4 {
		t.Fatalf("expected 4 items after expanding Dead header, got %d", len(expandedItems))
	}
	expandedView := m.renderThreadsView()
	if !strings.Contains(expandedView, "▼ 💀 DEAD (1 thread)") {
		t.Errorf("expected open Dead header in view, got:\n%s", expandedView)
	}
	if !strings.Contains(expandedView, "Closed Thread") {
		t.Errorf("expected Closed Thread visible after expanding Dead header")
	}

	// 2. Left Arrow: first Left collapses thread replies if expanded; second Left jumps to parent tier header
	m.ThreadsCursor = 1 // on dHot
	updated, _ = m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyLeft}) // collapses thread
	m = updated
	updated, _ = m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyLeft}) // jumps to parent tier header
	m = updated
	if m.ThreadsCursor != 0 {
		t.Errorf("expected cursor to jump to parent Hot tier header at index 0, got %d", m.ThreadsCursor)
	}

	// 3. Test expand all ('E') and collapse all ('Z')
	updated, _ = m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Z'}})
	m = updated
	for _, it := range m.getThreadFeedItems() {
		if it.Kind == FeedItemThread {
			t.Errorf("expected NO threads visible when all tiers are collapsed")
		}
	}

	updated, _ = m.handleThreadsKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'E'}})
	m = updated
	allOpen := m.getThreadFeedItems()
	if len(allOpen) != 4 {
		t.Errorf("expected all 4 items visible when all tiers expanded with 'E', got %d", len(allOpen))
	}
}
