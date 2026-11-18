package ui

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"grubby/internal/github"
	"grubby/internal/storage"
)

type TaskSubtab int

const (
	TaskSubtabMine TaskSubtab = iota
	TaskSubtabSquad
)

func (m Model) isSquadTasks() bool {
	if m.InlineAdding {
		return m.TaskCreateTarget == TaskTargetSquad
	}
	items := m.getVisibleTaskItems()
	if m.TaskCursor >= 0 && m.TaskCursor < len(items) {
		return items[m.TaskCursor].section == "squad"
	}
	return false
}

func (m Model) isTasksTab() bool {
	return m.ActiveTab == TabTasks
}

func sortTaskList(list []github.Task) []github.Task {
	slice := append([]github.Task{}, list...)
	sort.SliceStable(slice, func(i, j int) bool {
		rankI := slice[i].Priority.Rank()
		rankJ := slice[j].Priority.Rank()
		if rankI != rankJ {
			return rankI < rankJ
		}
		if !slice[i].UpdatedAt.Equal(slice[j].UpdatedAt) {
			return slice[i].UpdatedAt.After(slice[j].UpdatedAt)
		}
		return slice[i].Number > slice[j].Number
	})
	return slice
}

func (m Model) getSortedPersonalTasks() []github.Task {
	return sortTaskList(m.PersonalTasks)
}

func (m Model) getSortedSquadTasks() []github.Task {
	return sortTaskList(m.SquadTasks)
}

func (m Model) getAllOrderedTasks() []github.Task {
	return append(m.getSortedPersonalTasks(), m.getSortedSquadTasks()...)
}

func (m Model) getActiveTasks() []github.Task {
	return m.getAllOrderedTasks()
}

func (m Model) getDisplayTasks() []github.Task {
	return m.getActiveTasks()
}

func (m Model) getRecentDoneTasks() []github.Task {
	allDone := m.getSortedDoneTasks()
	if len(allDone) > 5 {
		return allDone[:5]
	}
	return allDone
}

type cursorLocationType int

const (
	locInlineNew cursorLocationType = iota
	locActiveTask
	locCompletedTask
)

type visibleTaskItem struct {
	navNum   int
	location cursorLocationType // locInlineNew, locActiveTask, locCompletedTask
	task     *github.Task
	section  string // "personal", "squad", "completed"
}

func (m Model) getVisibleTaskItems() []visibleTaskItem {
	var items []visibleTaskItem
	counter := 1

	// 1. Personal tasks
	if !m.TaskCollapsePersonal {
		if m.InlineAdding && m.TaskCreateTarget == TaskTargetPersonal {
			items = append(items, visibleTaskItem{
				navNum:   counter,
				location: locInlineNew,
				task:     nil,
				section:  "personal",
			})
			counter++
		}
		for _, t := range m.getSortedPersonalTasks() {
			taskCopy := t
			items = append(items, visibleTaskItem{
				navNum:   counter,
				location: locActiveTask,
				task:     &taskCopy,
				section:  "personal",
			})
			counter++
		}
	}

	// 2. Squad tasks
	if !m.TaskCollapseSquad {
		if m.InlineAdding && m.TaskCreateTarget == TaskTargetSquad {
			items = append(items, visibleTaskItem{
				navNum:   counter,
				location: locInlineNew,
				task:     nil,
				section:  "squad",
			})
			counter++
		}
		for _, t := range m.getSortedSquadTasks() {
			taskCopy := t
			items = append(items, visibleTaskItem{
				navNum:   counter,
				location: locActiveTask,
				task:     &taskCopy,
				section:  "squad",
			})
			counter++
		}
	}

	// 3. Completed tasks (ignored by [g] and cursor when collapsed!)
	if !m.TaskCollapseCompleted {
		for _, t := range m.getRecentDoneTasks() {
			taskCopy := t
			items = append(items, visibleTaskItem{
				navNum:   counter,
				location: locCompletedTask,
				task:     &taskCopy,
				section:  "completed",
			})
			counter++
		}
	}

	return items
}

func (m Model) getCursorTarget() (cursorLocationType, *github.Task) {
	items := m.getVisibleTaskItems()
	if len(items) == 0 {
		return locActiveTask, nil
	}
	idx := m.TaskCursor
	if idx < 0 {
		idx = 0
	}
	if idx >= len(items) {
		idx = len(items) - 1
	}
	return items[idx].location, items[idx].task
}

func (m Model) getTotalTaskRows() int {
	return len(m.getVisibleTaskItems())
}

func (m Model) renderTasksView() string {
	recentDone := m.getRecentDoneTasks()

	// 1. Task Board Header Banner
	mineOpen := len(m.PersonalTasks)
	squadOpen := len(m.SquadTasks)
	allDoneCount := len(m.getSortedDoneTasks())

	personalRepo := m.getTaskTargetRepoFullName(TaskTargetPersonal)
	squadRepo := m.getTaskTargetRepoFullName(TaskTargetSquad)

	boardBadge := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Render(
		fmt.Sprintf("📋 Tasks (Mine: %d  •  Squad: %d)", mineOpen, squadOpen),
	)

	var headerBanner string
	if m.TaskLeaderMode == TaskLeaderCreate {
		leaderNotice := lipgloss.NewStyle().Foreground(HighlightColor).Bold(true).Render("  •  [Add Task: Press [s] for Squad Task, [m] for My Task, [Esc] Cancel]")
		headerBanner = "  " + boardBadge + leaderNotice
	} else if m.TaskLeaderMode == TaskLeaderGo {
		bufStr := m.TaskGoBuffer
		if bufStr == "" {
			bufStr = "_"
		}
		leaderNotice := lipgloss.NewStyle().Foreground(HighlightColor).Bold(true).Render(fmt.Sprintf("  •  [Go to Task: Number [%s] + [Enter], [f] First, [l] Last, [Esc] Cancel]", bufStr))
		headerBanner = "  " + boardBadge + leaderNotice
	} else if m.InlineEditing {
		headerBanner = "  " + boardBadge + MetaBarStyle.Render("  •  [Editing Title: Enter to save, Esc to cancel]")
	} else if m.InlineAdding {
		targetName := "Squad Task"
		if m.TaskCreateTarget == TaskTargetPersonal {
			targetName = "My Task"
		}
		headerBanner = "  " + boardBadge + MetaBarStyle.Render(fmt.Sprintf("  •  [New %s: Enter to create, Esc to cancel]", targetName))
	} else {
		allDoneBadge := lipgloss.NewStyle().Foreground(lipgloss.Color("#8888AA")).Render(fmt.Sprintf("[d] All Done (%d)", allDoneCount))
		reposInfo := fmt.Sprintf("Mine: %s  •  Squad: %s", personalRepo, squadRepo)
		headerBanner = "  " + boardBadge + MetaBarStyle.Render(fmt.Sprintf("  •  %s  •  %s  •  [a] Add (s/m)  [g] Go to  [Space] Done", reposInfo, allDoneBadge))
	}

	// 2. Table Layout
	cursorW := 5
	prioW := 10
	notesW := 3
	assigneeW := 16
	updatedW := 12
	repoW := 14

	titleW := m.Width - (cursorW + prioW + notesW + assigneeW + updatedW + repoW + 6)
	if titleW < 24 {
		titleW = 24
	}

	cursorCol := lipgloss.NewStyle().Width(cursorW).MaxHeight(1).Inline(true)
	prioCol := lipgloss.NewStyle().Width(prioW).MaxHeight(1).Inline(true)
	titleCol := lipgloss.NewStyle().Width(titleW).MaxHeight(1).Inline(true)
	notesCol := lipgloss.NewStyle().Width(notesW).MaxHeight(1).Inline(true)
	assigneeCol := lipgloss.NewStyle().Width(assigneeW).MaxHeight(1).Inline(true)
	updatedCol := lipgloss.NewStyle().Width(updatedW).MaxHeight(1).Inline(true)
	repoCol := lipgloss.NewStyle().Width(repoW).MaxHeight(1).Inline(true)

	tableHead := lipgloss.JoinHorizontal(lipgloss.Top,
		cursorCol.Render(""),
		prioCol.Render("PRIORITY"),
		titleCol.Render("TASK TITLE"),
		notesCol.Render("📝"),
		assigneeCol.Render("ASSIGNEE"),
		updatedCol.Render("UPDATED"),
		repoCol.Render("ISSUE"),
	)
	renderedTableHead := TableHeaderStyle.Render(tableHead)

	formatTaskRow := func(t github.Task, isCurrent bool, navNum int) string {
		isFlashing, flashBg := false, lipgloss.Color("")
		if t.ID != 0 {
			isFlashing, flashBg = m.isEntityFlashing(fmt.Sprintf("task:%d", t.ID))
		}
		if !isFlashing {
			isFlashing, flashBg = m.isEntityFlashing(fmt.Sprintf("task:%s#%d", t.RepoName, t.Number))
		}

		var prioBadge string
		if t.IsPRReview() {
			switch t.Priority {
			case github.PriorityHigh:
				prioBadge = lipgloss.NewStyle().Bold(true).Foreground(DangerColor).Render("[⑂ HIGH]")
			case github.PriorityLow:
				prioBadge = lipgloss.NewStyle().Foreground(SuccessColor).Render("[⑂ LOW]")
			default:
				prioBadge = lipgloss.NewStyle().Foreground(WarningColor).Render("[⑂ MED]")
			}
		} else {
			switch t.Priority {
			case github.PriorityHigh:
				prioBadge = lipgloss.NewStyle().Bold(true).Foreground(DangerColor).Render("[🔴 HIGH]")
			case github.PriorityLow:
				prioBadge = lipgloss.NewStyle().Foreground(SuccessColor).Render("[🟢 LOW]")
			default:
				prioBadge = lipgloss.NewStyle().Foreground(WarningColor).Render("[🟡 MED]")
			}
		}

		var titleDisplay string
		if m.InlineEditing && t.ID == m.InlineEditTaskID {
			titleDisplay = m.InlineEditTitleInput.View()
		} else {
			titleDisplay = strings.ReplaceAll(strings.ReplaceAll(t.Title, "\r", " "), "\n", " ")
			if t.IsPRReview() {
				prPrefix := lipgloss.NewStyle().Foreground(lipgloss.Color("#A371F7")).Bold(true).Render("⑂ ")
				titleDisplay = prPrefix + titleDisplay
			}
			if len(titleDisplay) > titleW-2 && titleW > 3 {
				titleDisplay = titleDisplay[:titleW-3] + "…"
			}
			if isCurrent {
				titleDisplay = lipgloss.NewStyle().Bold(true).Render(titleDisplay)
			}
		}

		notesIcon := " · "
		if len(strings.TrimSpace(t.Body)) > 0 {
			notesIcon = "📝"
		}

		var assigneeText string
		if len(t.Assignees) > 0 {
			assigneeText = "@" + strings.Join(t.Assignees, ", @")
			if len(assigneeText) > assigneeW-2 && assigneeW > 3 {
				assigneeText = assigneeText[:assigneeW-3] + "…"
			}
			assigneeText = lipgloss.NewStyle().Foreground(SecondaryColor).Render(assigneeText)
		} else if t.IsPRReview() {
			assigneeText = lipgloss.NewStyle().Foreground(WarningColor).Bold(true).Render("Unassigned")
		} else {
			assigneeText = lipgloss.NewStyle().Foreground(MutedColor).Render("Unassigned")
		}

		updatedText := formatTimeAgo(t.UpdatedAt)

		issueRef := fmt.Sprintf("%s#%d", t.RepoName, t.Number)
		if t.IsHomeRepo {
			issueRef = fmt.Sprintf("🏠 %s#%d", t.RepoName, t.Number)
		}
		if len(issueRef) > repoW-2 && repoW > 3 {
			issueRef = issueRef[:repoW-3] + "…"
		}
		issueRefText := lipgloss.NewStyle().Foreground(HighlightColor).Render(issueRef)

		var cursorText string
		if m.TaskLeaderMode == TaskLeaderGo && navNum > 0 {
			if isCurrent {
				cursorText = lipgloss.NewStyle().Bold(true).Foreground(HighlightColor).Render(fmt.Sprintf("[%d]", navNum))
			} else {
				cursorText = lipgloss.NewStyle().Foreground(SecondaryColor).Render(fmt.Sprintf("[%d]", navNum))
			}
		} else if isFlashing {
			cursorText = "⚡ "
		} else if isCurrent {
			cursorText = "▶ "
		} else {
			cursorText = "  "
		}

		rowCells := lipgloss.JoinHorizontal(lipgloss.Top,
			cursorCol.Render(cursorText),
			prioCol.Render(prioBadge),
			titleCol.Render(titleDisplay),
			notesCol.Render(notesIcon),
			assigneeCol.Render(assigneeText),
			updatedCol.Render(updatedText),
			repoCol.Render(issueRefText),
		)

		if isCurrent {
			if isFlashing {
				return SelectedRowStyle.Background(flashBg).MaxHeight(1).Render(rowCells)
			}
			return SelectedRowStyle.MaxHeight(1).Render(rowCells)
		} else if isFlashing {
			return lipgloss.NewStyle().Background(flashBg).MaxHeight(1).Render(rowCells)
		}
		return NormalRowStyle.MaxHeight(1).Render(rowCells)
	}

	formatCompletedTaskRow := func(t github.Task, isCurrent bool, navNum int) string {
		var prioBadge string
		if t.IsPRReview() {
			switch t.Priority {
			case github.PriorityHigh:
				prioBadge = lipgloss.NewStyle().Foreground(DangerColor).Render("[✓⑂ HIGH]")
			case github.PriorityLow:
				prioBadge = lipgloss.NewStyle().Foreground(SuccessColor).Render("[✓⑂ LOW]")
			default:
				prioBadge = lipgloss.NewStyle().Foreground(WarningColor).Render("[✓⑂ MED]")
			}
		} else {
			switch t.Priority {
			case github.PriorityHigh:
				prioBadge = lipgloss.NewStyle().Foreground(DangerColor).Render("[✓ HIGH]")
			case github.PriorityLow:
				prioBadge = lipgloss.NewStyle().Foreground(SuccessColor).Render("[✓ LOW]")
			default:
				prioBadge = lipgloss.NewStyle().Foreground(WarningColor).Render("[✓ MED]")
			}
		}

		displayTitle := strings.ReplaceAll(strings.ReplaceAll(t.Title, "\r", " "), "\n", " ")
		if t.IsPRReview() {
			prPrefix := lipgloss.NewStyle().Foreground(lipgloss.Color("#A371F7")).Render("⑂ ")
			displayTitle = "✓ " + prPrefix + displayTitle
		} else {
			displayTitle = "✓ " + displayTitle
		}
		if len(displayTitle) > titleW-2 && titleW > 3 {
			displayTitle = displayTitle[:titleW-3] + "…"
		}
		if isCurrent {
			displayTitle = lipgloss.NewStyle().Bold(true).Render(displayTitle)
		} else {
			displayTitle = lipgloss.NewStyle().Foreground(MutedColor).Render(displayTitle)
		}

		notesIcon := " · "
		if len(strings.TrimSpace(t.Body)) > 0 {
			notesIcon = "📝"
		}

		var assigneeText string
		if len(t.Assignees) > 0 {
			assigneeText = "@" + strings.Join(t.Assignees, ", @")
			if len(assigneeText) > assigneeW-2 && assigneeW > 3 {
				assigneeText = assigneeText[:assigneeW-3] + "…"
			}
			assigneeText = lipgloss.NewStyle().Foreground(SecondaryColor).Render(assigneeText)
		} else {
			assigneeText = lipgloss.NewStyle().Foreground(MutedColor).Render("Unassigned")
		}

		var dateText string
		if t.ClosedAt != nil {
			dateText = formatTimeAgo(*t.ClosedAt)
		} else {
			dateText = formatTimeAgo(t.UpdatedAt)
		}

		issueRef := fmt.Sprintf("%s#%d", t.RepoName, t.Number)
		if t.IsHomeRepo {
			issueRef = fmt.Sprintf("🏠 %s#%d", t.RepoName, t.Number)
		}
		if len(issueRef) > repoW-2 && repoW > 3 {
			issueRef = issueRef[:repoW-3] + "…"
		}
		issueRefText := lipgloss.NewStyle().Foreground(HighlightColor).Render(issueRef)

		var cursorText string
		if m.TaskLeaderMode == TaskLeaderGo && navNum > 0 {
			if isCurrent {
				cursorText = lipgloss.NewStyle().Bold(true).Foreground(HighlightColor).Render(fmt.Sprintf("[%d]", navNum))
			} else {
				cursorText = lipgloss.NewStyle().Foreground(MutedColor).Render(fmt.Sprintf("[%d]", navNum))
			}
		} else if isCurrent {
			cursorText = "▶ "
		} else {
			cursorText = "  "
		}

		rowCells := lipgloss.JoinHorizontal(lipgloss.Top,
			cursorCol.Render(cursorText),
			prioCol.Render(prioBadge),
			titleCol.Render(displayTitle),
			notesCol.Render(notesIcon),
			assigneeCol.Render(assigneeText),
			updatedCol.Render(dateText),
			repoCol.Render(issueRefText),
		)

		if isCurrent {
			return SelectedRowStyle.MaxHeight(1).Render(rowCells)
		}
		return NormalRowStyle.MaxHeight(1).Render(rowCells)
	}

	var renderInlineAddingRow func(navNum int) string
	if m.InlineAdding {
		renderInlineAddingRow = func(navNum int) string {
			prioBadge := lipgloss.NewStyle().Bold(true).Foreground(WarningColor).Render("[🟡 MED]")
			switch m.InlineNewPriority {
			case github.PriorityHigh:
				prioBadge = lipgloss.NewStyle().Bold(true).Foreground(DangerColor).Render("[🔴 HIGH]")
			case github.PriorityLow:
				prioBadge = lipgloss.NewStyle().Foreground(SuccessColor).Render("[🟢 LOW]")
			}

			notesIcon := " · "
			if len(strings.TrimSpace(m.InlineNewNotes)) > 0 {
				notesIcon = "📝"
			}

			assigneeDisplay := "Unassigned"
			if m.InlineNewAssignee != "" {
				assigneeDisplay = "@" + m.InlineNewAssignee
			}
			assigneeText := lipgloss.NewStyle().Foreground(SecondaryColor).Render(assigneeDisplay)
			dateText := lipgloss.NewStyle().Foreground(MutedColor).Render("Draft")
			issueText := lipgloss.NewStyle().Foreground(WarningColor).Render("(new)")

			cursorText := "▶ "
			if m.TaskLeaderMode == TaskLeaderGo && navNum > 0 {
				cursorText = lipgloss.NewStyle().Bold(true).Foreground(HighlightColor).Render(fmt.Sprintf("[%d]", navNum))
			}

			inlineCells := lipgloss.JoinHorizontal(lipgloss.Top,
				cursorCol.Render(cursorText),
				prioCol.Render(prioBadge),
				titleCol.Render(m.InlineNewTitleInput.View()),
				notesCol.Render(notesIcon),
				assigneeCol.Render(assigneeText),
				updatedCol.Render(dateText),
				repoCol.Render(issueText),
			)
			return SelectedRowStyle.MaxHeight(1).Render(inlineCells)
		}
	}

	personal := m.getSortedPersonalTasks()
	squad := m.getSortedSquadTasks()
	visibleItems := m.getVisibleTaskItems()

	var rows []string
	rows = append(rows, headerBanner)

	// 1. MY TASKS TABLE
	var personalIcon string
	if m.TaskCollapsePersonal {
		personalIcon = "▶"
	} else {
		personalIcon = "▼"
	}
	sepPersonal := lipgloss.NewStyle().Foreground(SecondaryColor).Bold(true).Render(
		fmt.Sprintf("─── %s 📋 MY TASKS (%d) ──────────────────────────────────────────────────────────", personalIcon, len(personal)),
	)
	rows = append(rows, "", sepPersonal)

	if !m.TaskCollapsePersonal {
		rows = append(rows, renderedTableHead)
		if m.InlineAdding && m.TaskCreateTarget == TaskTargetPersonal {
			navNum := 1
			for _, item := range visibleItems {
				if item.location == locInlineNew && item.section == "personal" {
					navNum = item.navNum
					break
				}
			}
			rows = append(rows, renderInlineAddingRow(navNum))
		}

		if len(personal) == 0 && (!m.InlineAdding || m.TaskCreateTarget != TaskTargetPersonal) {
			rows = append(rows, lipgloss.NewStyle().Foreground(MutedColor).Render("   No active personal tasks. Press [a] then [m] to add a task!"))
		} else {
			for _, item := range visibleItems {
				if item.section == "personal" && item.location == locActiveTask && item.task != nil {
					isCurrent := (item.navNum-1 == m.TaskCursor)
					rows = append(rows, formatTaskRow(*item.task, isCurrent, item.navNum))
				}
			}
		}
	}

	// 2. SQUAD TASKS TABLE
	var squadIcon string
	if m.TaskCollapseSquad {
		squadIcon = "▶"
	} else {
		squadIcon = "▼"
	}
	sepSquad := lipgloss.NewStyle().Foreground(HighlightColor).Bold(true).Render(
		fmt.Sprintf("─── %s 👥 SQUAD TASKS (%d) ──────────────────────────────────────────────────────────", squadIcon, len(squad)),
	)
	rows = append(rows, "", sepSquad)

	if !m.TaskCollapseSquad {
		rows = append(rows, renderedTableHead)
		if m.InlineAdding && m.TaskCreateTarget == TaskTargetSquad {
			navNum := 1
			for _, item := range visibleItems {
				if item.location == locInlineNew && item.section == "squad" {
					navNum = item.navNum
					break
				}
			}
			rows = append(rows, renderInlineAddingRow(navNum))
		}

		if len(squad) == 0 && (!m.InlineAdding || m.TaskCreateTarget != TaskTargetSquad) {
			rows = append(rows, lipgloss.NewStyle().Foreground(MutedColor).Render("   No active squad tasks. Press [a] then [s] to add a task!"))
		} else {
			for _, item := range visibleItems {
				if item.section == "squad" && item.location == locActiveTask && item.task != nil {
					isCurrent := (item.navNum-1 == m.TaskCursor)
					rows = append(rows, formatTaskRow(*item.task, isCurrent, item.navNum))
				}
			}
		}
	}

	// Completed Tasks Section (Last 5)
	if len(recentDone) > 0 {
		if m.TaskCollapseCompleted {
			sepText := fmt.Sprintf("─── ▶ COMPLETED (%d • [D] Expand • [d] Show All) ───────────────────────────────────────────────────", allDoneCount)
			sep := lipgloss.NewStyle().Foreground(lipgloss.Color("#555566")).Render(sepText)
			rows = append(rows, "", sep)
		} else {
			sepText := fmt.Sprintf("─── ▼ COMPLETED (Last %d • [D] Collapse • [d] Show All %d) ──────────────────────────────────────────", len(recentDone), allDoneCount)
			sep := lipgloss.NewStyle().Foreground(lipgloss.Color("#555566")).Render(sepText)
			rows = append(rows, "", sep)

			for _, item := range visibleItems {
				if item.section == "completed" && item.task != nil {
					isCurrent := (item.navNum-1 == m.TaskCursor)
					rows = append(rows, formatCompletedTaskRow(*item.task, isCurrent, item.navNum))
				}
			}
		}
	}

	// Exactly one empty row between task table and bottom hot-key row
	rows = append(rows, "")

	return strings.Join(rows, "\n")
}

func (m Model) handleTasksKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	totalRows := m.getTotalTaskRows()

	// 1. Handling keys while Inline Adding
	if m.InlineAdding {
		switch msg.String() {
		case "esc", "ctrl+c":
			m.InlineAdding = false
			m.InlineNewNotes = ""
			m.InlineNewTitleInput.Reset()
			m.StatusMsg = "Cancelled adding task"
			m.StatusMsgTimer = time.Now().Add(2 * time.Second)
			return m, nil

		case "enter":
			titleText := strings.TrimSpace(m.InlineNewTitleInput.Value())
			if titleText == "" {
				m.InlineAdding = false
				return m, nil
			}
			target := TaskTargetPersonal
			isSquad := false
			if m.isSquadTasks() {
				target = TaskTargetSquad
				isSquad = true
			}
			repoFullName := m.getTaskTargetRepoFullName(target)
			parts := strings.SplitN(repoFullName, "/", 2)
			owner := parts[0]
			repo := parts[1]

			var assignees []string
			if !isSquad {
				if m.AuthUser != "" {
					assignees = append(assignees, m.AuthUser)
				}
			} else if m.InlineNewAssignee != "" {
				assignees = append(assignees, m.InlineNewAssignee)
			}

			priority := m.InlineNewPriority
			if priority == "" {
				priority = github.PriorityMedium
			}
			body := m.InlineNewNotes

			now := time.Now()
			taskLabels := []string{priority.Label()}
			if isSquad && m.Config != nil && m.Config.ActiveSquad != "" {
				taskLabels = append(taskLabels, "squad:"+strings.ToLower(strings.TrimSpace(m.Config.ActiveSquad)))
			}
			tempTask := github.Task{
				ID:          -now.UnixNano(),
				Number:      0,
				Title:       titleText,
				Body:        body,
				State:       "open",
				Priority:    priority,
				RepoOwner:   owner,
				RepoName:    repo,
				Author:      m.AuthUser,
				Assignees:   assignees,
				Labels:      taskLabels,
				CreatedAt:   now,
				UpdatedAt:   now,
				IsSquadTask: isSquad,
			}
			if isSquad {
				m.SquadTasks = append([]github.Task{tempTask}, m.SquadTasks...)
			} else {
				m.PersonalTasks = append([]github.Task{tempTask}, m.PersonalTasks...)
			}
			if m.DB != nil {
				_ = m.DB.SaveTask(&tempTask, "pending_create")
			}
			m.InlineAdding = false
			m.InlineNewNotes = ""
			m.InlineNewTitleInput.Reset()
			m.TaskCursor = 0
			m.StatusMsg = fmt.Sprintf("✓ Created task draft in %s (syncing with GitHub...)", repoFullName)
			m.StatusMsgTimer = time.Now().Add(3 * time.Second)
			return m, m.createTaskCmd(owner, repo, titleText, body, priority, assignees, isSquad)

		case "ctrl+n":
			return m.openTaskNotesModal(nil, true)

		case "ctrl+p":
			switch m.InlineNewPriority {
			case github.PriorityMedium:
				m.InlineNewPriority = github.PriorityHigh
			case github.PriorityHigh:
				m.InlineNewPriority = github.PriorityLow
			default:
				m.InlineNewPriority = github.PriorityMedium
			}
			m.StatusMsg = fmt.Sprintf("Priority set to %s for new task", m.InlineNewPriority)
			return m, nil

		case "tab":
			if m.isSquadTasks() {
				candidates := m.getSquadTaskAssigneeCandidates()
				curIdx := 0
				for i, c := range candidates {
					if c == m.InlineNewAssignee {
						curIdx = i
						break
					}
				}
				m.InlineNewAssignee = candidates[(curIdx+1)%len(candidates)]
				return m, nil
			}
		}

		var cmd tea.Cmd
		m.InlineNewTitleInput, cmd = m.InlineNewTitleInput.Update(msg)
		return m, cmd
	}

	// 2. Handling keys while Inline Editing an existing task's title
	if m.InlineEditing {
		switch msg.String() {
		case "esc", "ctrl+c":
			m.InlineEditing = false
			m.StatusMsg = "Cancelled editing title"
			m.StatusMsgTimer = time.Now().Add(2 * time.Second)
			return m, nil

		case "enter":
			newTitle := strings.TrimSpace(m.InlineEditTitleInput.Value())
			m.InlineEditing = false
			if newTitle != "" {
				for _, t := range m.getActiveTasks() {
					if t.ID == m.InlineEditTaskID {
						target := t
						target.Title = newTitle
						target.UpdatedAt = time.Now()
						m = m.updateTaskInSlices(target)
						if m.DB != nil {
							_ = m.DB.SaveTask(&target, "pending_update")
						}
						m.StatusMsg = fmt.Sprintf("✓ Saved title for task #%d", target.Number)
						m.StatusMsgTimer = time.Now().Add(3 * time.Second)

						if m.Debouncer != nil {
							m.Debouncer.Enqueue(storage.TaskDebounceJob{
								TaskID:    target.ID,
								Number:    target.Number,
								RepoOwner: target.RepoOwner,
								RepoName:  target.RepoName,
								Title:     target.Title,
								Body:      target.Body,
								Priority:  target.Priority,
								Assignees: target.Assignees,
								Labels:    target.Labels,
								IsSquad:   target.IsSquadTask,
								Action:    storage.ActionUpdateTask,
							}, func(ctx context.Context, job *storage.TaskDebounceJob) error {
								var err error
								if m.GHClient != nil {
									_, err = m.GHClient.UpdateTask(ctx, job.RepoOwner, job.RepoName, job.Number, job.Title, job.Body, job.Priority, job.Assignees, job.Labels, job.IsSquad)
								}
								if err == nil && m.DB != nil {
									_ = m.DB.UpdateTaskSyncStatus(job.TaskID, "synced")
								}
								return err
							})
							return m, nil
						}

						return m, m.updateTaskCmd(t, newTitle, t.Body, t.Priority, t.Assignees)
					}
				}
			}
			return m, nil

		case "ctrl+n":
			for _, t := range m.getActiveTasks() {
				if t.ID == m.InlineEditTaskID {
					target := t
					return m.openTaskNotesModal(&target, false)
				}
			}
		}

		var cmd tea.Cmd
		m.InlineEditTitleInput, cmd = m.InlineEditTitleInput.Update(msg)
		return m, cmd
	}

	// 2b. Handle TaskLeaderCreate mode ('a' leader)
	if m.TaskLeaderMode == TaskLeaderCreate {
		switch msg.String() {
		case "s", "S":
			m.TaskLeaderMode = TaskLeaderNone
			m.InlineAdding = true
			m.TaskCreateTarget = TaskTargetSquad
			m.InlineNewNotes = ""
			m.InlineNewPriority = github.PriorityMedium
			m.InlineNewAssignee = ""
			m.TaskCursor = len(m.getSortedPersonalTasks())
			ti := textinput.New()
			ti.Placeholder = "What needs to be done for squad? (Enter to create, Esc to cancel)"
			ti.Prompt = ""
			ti.Focus()
			m.InlineNewTitleInput = ti
			m.StatusMsg = "Creating Squad Task in " + m.getTaskTargetRepoFullName(TaskTargetSquad)
			m.StatusMsgTimer = time.Now().Add(3 * time.Second)
			return m, textinput.Blink

		case "m", "M":
			m.TaskLeaderMode = TaskLeaderNone
			m.InlineAdding = true
			m.TaskCreateTarget = TaskTargetPersonal
			m.InlineNewNotes = ""
			m.InlineNewPriority = github.PriorityMedium
			m.InlineNewAssignee = m.AuthUser
			m.TaskCursor = 0
			ti := textinput.New()
			ti.Placeholder = "What needs to be done? (Enter to create, Esc to cancel)"
			ti.Prompt = ""
			ti.Focus()
			m.InlineNewTitleInput = ti
			m.StatusMsg = "Creating My Task in " + m.getTaskTargetRepoFullName(TaskTargetPersonal)
			m.StatusMsgTimer = time.Now().Add(3 * time.Second)
			return m, textinput.Blink

		case "esc":
			m.TaskLeaderMode = TaskLeaderNone
			m.StatusMsg = "Cancelled task creation"
			m.StatusMsgTimer = time.Now().Add(1500 * time.Millisecond)
			return m, nil

		default:
			m.StatusMsg = "Press [s] for Squad Task, [m] for My Task, or [Esc] to cancel"
			m.StatusMsgTimer = time.Now().Add(2 * time.Second)
			return m, nil
		}
	}

	// 2c. Handle TaskLeaderGo mode ('g' leader)
	if m.TaskLeaderMode == TaskLeaderGo {
		switch msg.String() {
		case "f", "g":
			m.TaskLeaderMode = TaskLeaderNone
			m.TaskGoBuffer = ""
			m.TaskCursor = 0
			m.StatusMsg = "Jumped to first task"
			m.StatusMsgTimer = time.Now().Add(2 * time.Second)
			return m, nil

		case "l", "G":
			m.TaskLeaderMode = TaskLeaderNone
			m.TaskGoBuffer = ""
			if totalRows > 0 {
				m.TaskCursor = totalRows - 1
			}
			m.StatusMsg = "Jumped to last task"
			m.StatusMsgTimer = time.Now().Add(2 * time.Second)
			return m, nil

		case "0", "1", "2", "3", "4", "5", "6", "7", "8", "9":
			m.TaskGoBuffer += msg.String()
			m.StatusMsg = fmt.Sprintf("Go to task: %s (press Enter)", m.TaskGoBuffer)
			m.StatusMsgTimer = time.Now().Add(5 * time.Second)
			return m, nil

		case "backspace":
			if len(m.TaskGoBuffer) > 0 {
				m.TaskGoBuffer = m.TaskGoBuffer[:len(m.TaskGoBuffer)-1]
				m.StatusMsg = fmt.Sprintf("Go to task: %s (press Enter)", m.TaskGoBuffer)
				m.StatusMsgTimer = time.Now().Add(5 * time.Second)
			}
			return m, nil

		case "enter":
			buf := m.TaskGoBuffer
			m.TaskLeaderMode = TaskLeaderNone
			m.TaskGoBuffer = ""
			targetNum, err := strconv.Atoi(buf)
			if err != nil || targetNum <= 0 {
				m.StatusMsg = "Invalid task number"
				m.StatusMsgTimer = time.Now().Add(2 * time.Second)
				return m, nil
			}

			visibleItems := m.getVisibleTaskItems()
			foundIdx := -1
			var foundTask *github.Task

			// 1. Check if targetNum matches the visible label number [1], [2], etc.
			for idx, item := range visibleItems {
				if item.navNum == targetNum {
					foundIdx = idx
					foundTask = item.task
					break
				}
			}
			// 2. If not found by nav label, check if targetNum matches GitHub issue Number among VISIBLE items
			if foundIdx == -1 {
				for idx, item := range visibleItems {
					if item.task != nil && item.task.Number == targetNum {
						foundIdx = idx
						foundTask = item.task
						break
					}
				}
			}

			if foundIdx != -1 {
				m.TaskCursor = foundIdx
				if foundTask != nil {
					m.StatusMsg = fmt.Sprintf("Moved to task [%d] #%d", targetNum, foundTask.Number)
				} else {
					m.StatusMsg = fmt.Sprintf("Moved to item [%d]", targetNum)
				}
				m.StatusMsgTimer = time.Now().Add(2 * time.Second)
			} else {
				m.StatusMsg = fmt.Sprintf("Task [%d] not found or collapsed", targetNum)
				m.StatusMsgTimer = time.Now().Add(2 * time.Second)
			}
			return m, nil

		case "esc":
			m.TaskLeaderMode = TaskLeaderNone
			m.TaskGoBuffer = ""
			m.StatusMsg = "Cancelled go to task"
			m.StatusMsgTimer = time.Now().Add(1500 * time.Millisecond)
			return m, nil

		default:
			m.StatusMsg = fmt.Sprintf("Go to task: %s (digits, [Enter], [f]irst, [l]ast, [Esc] cancel)", m.TaskGoBuffer)
			m.StatusMsgTimer = time.Now().Add(2 * time.Second)
			return m, nil
		}
	}

	// 3. Normal Task Navigation & Actions
	switch msg.String() {
	case "up", "k":
		if m.TaskCursor > 0 {
			m.TaskCursor--
		}

	case "down", "j":
		if m.TaskCursor < totalRows-1 {
			m.TaskCursor++
		}

	case "pgdown", "ctrl+d":
		m.TaskCursor += 8
		if m.TaskCursor >= totalRows {
			m.TaskCursor = totalRows - 1
		}
		if m.TaskCursor < 0 {
			m.TaskCursor = 0
		}

	case "pgup", "ctrl+u":
		m.TaskCursor -= 8
		if m.TaskCursor < 0 {
			m.TaskCursor = 0
		}

	case "home":
		m.TaskCursor = 0

	case "end", "G":
		if totalRows > 0 {
			m.TaskCursor = totalRows - 1
		}

	case "left", "h", "[":
		// Jump to top of My Tasks
		m.TaskCursor = 0
		return m, nil

	case "right", "l", "]":
		// Jump to top of Squad Tasks
		personalCount := len(m.getSortedPersonalTasks())
		if totalRows > personalCount {
			m.TaskCursor = personalCount
		}
		return m, nil

	case "a":
		m.TaskLeaderMode = TaskLeaderCreate
		m.StatusMsg = "Add Task: [s] Squad Task  •  [m] My Task  •  [Esc] Cancel"
		m.StatusMsgTimer = time.Now().Add(4 * time.Second)
		return m, nil

	case "g":
		m.TaskLeaderMode = TaskLeaderGo
		m.TaskGoBuffer = ""
		m.StatusMsg = "Go to Task: [f] First  •  [l] Last  •  Type Number + [Enter]  •  [Esc] Cancel"
		m.StatusMsgTimer = time.Now().Add(5 * time.Second)
		return m, nil

	case "enter", "e":
		loc, task := m.getCursorTarget()
		if task != nil {
			if task.IsPRReview() {
				targetURL := task.PRURL()
				if targetURL == "" {
					targetURL = task.URL
				}
				_ = OpenBrowser(targetURL)

				// If an unassigned or multi-assigned review task is launched, claim it for the current user
				if (len(task.Assignees) == 0 || len(task.Assignees) > 1) && m.AuthUser != "" {
					newAssignees := []string{m.AuthUser}
					taskCopy := *task
					taskCopy.Assignees = newAssignees
					m = m.updateTaskInSlices(taskCopy)
					assignCmd := m.updateTaskAssigneesCmd(taskCopy, newAssignees)
					return m, tea.Batch(
						func() tea.Msg {
							return StatusToastMsg(fmt.Sprintf("Opened PR & claimed review task #%d (assigned to @%s)", task.Number, m.AuthUser))
						},
						assignCmd,
					)
				}

				return m, func() tea.Msg {
					return StatusToastMsg(fmt.Sprintf("Opened PR in browser: %s", targetURL))
				}
			}
			if loc == locActiveTask {
				m.InlineEditing = true
				m.InlineEditTaskID = task.ID
				ieti := textinput.New()
				ieti.SetValue(task.Title)
				ieti.Prompt = ""
				ieti.Focus()
				m.InlineEditTitleInput = ieti
				return m, textinput.Blink
			}
		}

	case " ", "x":
		loc, task := m.getCursorTarget()
		if task != nil {
			if loc == locCompletedTask {
				target := *task
				m = m.reopenTaskLocally(target)
				if m.DB != nil {
					_ = m.DB.SaveTask(&target, "pending_reopen")
				}
				m.StatusMsg = fmt.Sprintf("✓ Reopened task #%d in %s", task.Number, task.RepoName)
				m.StatusMsgTimer = time.Now().Add(3 * time.Second)
				return m, m.reopenTaskCmd(*task)
			} else if loc == locActiveTask {
				target := *task
				m = m.closeTaskLocally(target)
				if m.DB != nil {
					_ = m.DB.SaveTask(&target, "pending_close")
				}
				m.StatusMsg = fmt.Sprintf("✓ Completed task #%d in %s", task.Number, task.RepoName)
				m.StatusMsgTimer = time.Now().Add(3 * time.Second)
				return m, m.closeTaskCmd(*task)
			}
		}

	case "n":
		loc, task := m.getCursorTarget()
		if loc == locInlineNew {
			return m.openTaskNotesModal(nil, true)
		} else if task != nil {
			return m.openTaskNotesModal(task, false)
		}

	case "p":
		loc, task := m.getCursorTarget()
		if loc == locInlineNew {
			switch m.InlineNewPriority {
			case github.PriorityMedium:
				m.InlineNewPriority = github.PriorityHigh
			case github.PriorityHigh:
				m.InlineNewPriority = github.PriorityLow
			default:
				m.InlineNewPriority = github.PriorityMedium
			}
			m.StatusMsg = fmt.Sprintf("Priority set to %s for new task", m.InlineNewPriority)
			return m, nil
		} else if task != nil && loc == locActiveTask {
			var nextPriority github.TaskPriority
			switch task.Priority {
			case github.PriorityMedium:
				nextPriority = github.PriorityHigh
			case github.PriorityHigh:
				nextPriority = github.PriorityLow
			default:
				nextPriority = github.PriorityMedium
			}
			target := *task
			target.Priority = nextPriority
			target.UpdatedAt = time.Now()
			m = m.updateTaskInSlices(target)
			if m.DB != nil {
				_ = m.DB.SaveTask(&target, "pending_update")
			}
			m.StatusMsg = fmt.Sprintf("✓ Priority set to %s for task #%d", nextPriority, task.Number)
			m.StatusMsgTimer = time.Now().Add(3 * time.Second)

			if m.Debouncer != nil {
				m.Debouncer.Enqueue(storage.TaskDebounceJob{
					TaskID:    target.ID,
					Number:    target.Number,
					RepoOwner: target.RepoOwner,
					RepoName:  target.RepoName,
					Title:     target.Title,
					Body:      target.Body,
					Priority:  target.Priority,
					Assignees: target.Assignees,
					Labels:    target.Labels,
					IsSquad:   target.IsSquadTask,
					Action:    storage.ActionPriorityTask,
				}, func(ctx context.Context, job *storage.TaskDebounceJob) error {
					var err error
					if m.GHClient != nil {
						err = m.GHClient.UpdateTaskPriority(ctx, job.RepoOwner, job.RepoName, job.Number, job.Priority, job.Labels)
					}
					if err == nil && m.DB != nil {
						_ = m.DB.UpdateTaskSyncStatus(job.TaskID, "synced")
					}
					return err
				})
				return m, nil
			}

			return m, m.updateTaskPriorityCmd(*task, nextPriority)
		}

	case "A":
		if m.isSquadTasks() {
			loc, task := m.getCursorTarget()
			if task != nil && loc == locActiveTask {
				m.ActiveModal = ModalAssignSquadTask
				m.AssignTaskTarget = task
				m.AssignTaskCandidates = m.getSquadAssigneeOptions()
				m.AssignTaskMemberCursor = 0
				m.AssignTaskFocusedField = 0
				m.AssignTaskActiveButton = 0
				m.AssignTaskSelectedMembers = make(map[string]bool)
				for _, a := range task.Assignees {
					m.AssignTaskSelectedMembers[a] = true
				}
				return m, nil
			}
		}

	case "c":
		loc, task := m.getCursorTarget()
		candidates := m.getSquadTaskAssigneeCandidates()
		if loc == locInlineNew && m.isSquadTasks() {
			currentIdx := 0
			for i, c := range candidates {
				if c == m.InlineNewAssignee {
					currentIdx = i
					break
				}
			}
			m.InlineNewAssignee = candidates[(currentIdx+1)%len(candidates)]
			return m, nil
		} else if task != nil && task.IsSquadTask && loc == locActiveTask {
			currentAssignee := ""
			if len(task.Assignees) > 0 {
				currentAssignee = task.Assignees[0]
			}
			currentIdx := 0
			for i, c := range candidates {
				if c == currentAssignee {
					currentIdx = i
					break
				}
			}
			nextAssignee := candidates[(currentIdx+1)%len(candidates)]
			var newAssignees []string
			if nextAssignee != "" {
				newAssignees = append(newAssignees, nextAssignee)
			}
			target := *task
			target.Assignees = newAssignees
			target.UpdatedAt = time.Now()
			m = m.updateTaskInSlices(target)
			if m.DB != nil {
				_ = m.DB.SaveTask(&target, "pending_update")
			}
			m.StatusMsg = fmt.Sprintf("✓ Assigned task #%d to %s", target.Number, nextAssignee)
			m.StatusMsgTimer = time.Now().Add(3 * time.Second)

			if m.Debouncer != nil {
				m.Debouncer.Enqueue(storage.TaskDebounceJob{
					TaskID:    target.ID,
					Number:    target.Number,
					RepoOwner: target.RepoOwner,
					RepoName:  target.RepoName,
					Title:     target.Title,
					Body:      target.Body,
					Priority:  target.Priority,
					Assignees: target.Assignees,
					Labels:    target.Labels,
					IsSquad:   target.IsSquadTask,
					Action:    storage.ActionAssignTask,
				}, func(ctx context.Context, job *storage.TaskDebounceJob) error {
					var err error
					if m.GHClient != nil {
						err = m.GHClient.UpdateTaskAssignees(ctx, job.RepoOwner, job.RepoName, job.Number, job.Assignees)
					}
					if err == nil && m.DB != nil {
						_ = m.DB.UpdateTaskSyncStatus(job.TaskID, "synced")
					}
					return err
				})
				return m, nil
			}

			return m, m.updateTaskAssigneesCmd(*task, newAssignees)
		}

	case "d":
		// Open Completed Tasks modal
		m.ActiveModal = ModalCompletedTasks
		m.CompletedTasksCursor = 0
		return m, nil

	case "D":
		m.TaskCollapseCompleted = !m.TaskCollapseCompleted
		if m.TaskCollapseCompleted {
			m.StatusMsg = "Collapsed completed tasks"
		} else {
			m.StatusMsg = "Expanded completed tasks"
		}
		m.StatusMsgTimer = time.Now().Add(2 * time.Second)
		visibleCount := len(m.getVisibleTaskItems())
		if m.TaskCursor >= visibleCount && visibleCount > 0 {
			m.TaskCursor = visibleCount - 1
		}
		return m, nil

	case "z":
		visibleItems := m.getVisibleTaskItems()
		if len(visibleItems) > 0 && m.TaskCursor < len(visibleItems) {
			sec := visibleItems[m.TaskCursor].section
			switch sec {
			case "personal":
				m.TaskCollapsePersonal = !m.TaskCollapsePersonal
			case "squad":
				m.TaskCollapseSquad = !m.TaskCollapseSquad
			case "completed":
				m.TaskCollapseCompleted = !m.TaskCollapseCompleted
			}
			visibleCount := len(m.getVisibleTaskItems())
			if m.TaskCursor >= visibleCount && visibleCount > 0 {
				m.TaskCursor = visibleCount - 1
			}
		}
		return m, nil

	case "i":
		// Always open the underlying GitHub issue in browser
		_, task := m.getCursorTarget()
		if task != nil && task.URL != "" {
			_ = OpenBrowser(task.URL)
			return m, func() tea.Msg {
				return StatusToastMsg(fmt.Sprintf("Opened issue #%d in browser", task.Number))
			}
		}

	case "o":
		// Open in browser
		_, task := m.getCursorTarget()
		if task != nil {
			targetURL := task.URL
			if task.IsPRReview() {
				if prURL := task.PRURL(); prURL != "" {
					targetURL = prURL
				}
				_ = OpenBrowser(targetURL)

				if (len(task.Assignees) == 0 || len(task.Assignees) > 1) && m.AuthUser != "" {
					newAssignees := []string{m.AuthUser}
					taskCopy := *task
					taskCopy.Assignees = newAssignees
					m = m.updateTaskInSlices(taskCopy)
					assignCmd := m.updateTaskAssigneesCmd(taskCopy, newAssignees)
					return m, tea.Batch(
						func() tea.Msg {
							return StatusToastMsg(fmt.Sprintf("Opened PR & claimed review task #%d (assigned to @%s)", task.Number, m.AuthUser))
						},
						assignCmd,
					)
				}

				return m, func() tea.Msg {
					return StatusToastMsg(fmt.Sprintf("Opened in browser: %s", targetURL))
				}
			}
			if targetURL != "" {
				_ = OpenBrowser(targetURL)
				return m, func() tea.Msg {
					return StatusToastMsg(fmt.Sprintf("Opened in browser: %s", targetURL))
				}
			}
		}
	}

	return m, nil
}
