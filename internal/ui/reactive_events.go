package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"grubby/internal/eventbus"
	"grubby/internal/github"
	"grubby/internal/protocol"
	"grubby/internal/storage"
)

type EntityEventMsg protocol.EventEnvelope

func listenForEventsCmd(sub *eventbus.Subscription) tea.Cmd {
	if sub == nil {
		return nil
	}
	return func() tea.Msg {
		event, ok := <-sub.C
		if !ok {
			return nil
		}
		return EntityEventMsg(event)
	}
}

func (m Model) handleEntityEvent(env protocol.EventEnvelope) (Model, tea.Cmd) {
	switch env.Entity {
	case protocol.EntityRepository:
		return m.patchRepository(env)
	case protocol.EntityPullRequest:
		return m.patchPullRequest(env)
	case protocol.EntityTask:
		return m.patchTask(env)
	case protocol.EntitySquadActivity:
		return m.patchSquadActivity(env)
	default:
		return m, nil
	}
}

func (m Model) patchRepository(env protocol.EventEnvelope) (Model, tea.Cmd) {
	var payload protocol.RepositoryPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		return m, nil
	}
	repo := payload.Repo
	if repo.Name == "" && repo.FullName != "" {
		parts := strings.Split(repo.FullName, "/")
		if len(parts) == 2 {
			repo.Owner = parts[0]
			repo.Name = parts[1]
		}
	}

	switch env.Action {
	case protocol.ActionDeleted:
		var updated []github.Repository
		for _, r := range m.Repos {
			if strings.EqualFold(r.FullName, repo.FullName) || (repo.FullName == "" && strings.EqualFold(r.Name, repo.Name)) {
				continue
			}
			updated = append(updated, r)
		}
		m.Repos = updated
		if m.RepoCursor >= len(m.Repos) && len(m.Repos) > 0 {
			m.RepoCursor = len(m.Repos) - 1
		}
		m.StatusMsg = fmt.Sprintf("⚡ [Repository] %s deleted", repo.FullName)
		m.StatusMsgTimer = time.Now().Add(3 * time.Second)

	case protocol.ActionCreated, protocol.ActionUpdated:
		found := false
		for i, r := range m.Repos {
			if (repo.FullName != "" && strings.EqualFold(r.FullName, repo.FullName)) ||
				(r.FullName == repo.FullName && strings.EqualFold(r.Name, repo.Name)) {
				// Update existing
				if repo.Description != "" || repo.PushedAt.After(r.PushedAt) {
					m.Repos[i] = repo
				} else {
					if !repo.PushedAt.IsZero() {
						m.Repos[i].PushedAt = repo.PushedAt
					}
					if !repo.UpdatedAt.IsZero() {
						m.Repos[i].UpdatedAt = repo.UpdatedAt
					}
					if repo.Owner != "" {
						m.Repos[i].Owner = repo.Owner
					}
					if repo.FullName != "" {
						m.Repos[i].FullName = repo.FullName
					}
					if repo.DefaultBranch != "" {
						m.Repos[i].DefaultBranch = repo.DefaultBranch
					}
					if repo.Description != "" {
						m.Repos[i].Description = repo.Description
					}
				}
				found = true
				break
			}
		}
		if !found {
			// Prepend new repo
			m.Repos = append([]github.Repository{repo}, m.Repos...)
			if m.RepoTiers != nil {
				m.RepoTiers[repo.Name] = storage.TierWarm
			}
			if m.RepoInteractions != nil {
				m.RepoInteractions[repo.Name] = time.Now()
			}
		}
		actionWord := "updated"
		if env.Action == protocol.ActionCreated {
			actionWord = "created"
		}
		disp := repo.FullName
		if disp == "" {
			disp = repo.Name
		}
		m.recordEntityFlash("repo:" + repo.FullName)
		m.recordEntityFlash("repo:" + repo.Name)
		m.StatusMsg = fmt.Sprintf("⚡ [Repository] %s %s", disp, actionWord)
		m.StatusMsgTimer = time.Now().Add(3 * time.Second)
	}
	return m, nil
}

func (m Model) patchPullRequest(env protocol.EventEnvelope) (Model, tea.Cmd) {
	var payload protocol.PullRequestPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		return m, nil
	}
	pr := payload.PR
	isClosedOrMerged := strings.EqualFold(pr.State, "closed") || strings.EqualFold(pr.State, "merged")

	switch env.Action {
	case protocol.ActionDeleted:
		var updated []github.PullRequest
		for _, p := range m.PRs {
			if isSamePR(p, pr) {
				continue
			}
			updated = append(updated, p)
		}
		m.PRs = updated
		if m.PRCursor >= len(m.PRs) && len(m.PRs) > 0 {
			m.PRCursor = len(m.PRs) - 1
		}
		m.StatusMsg = fmt.Sprintf("⚡ [PR] #%d deleted", pr.Number)
		m.StatusMsgTimer = time.Now().Add(3 * time.Second)

	case protocol.ActionCreated, protocol.ActionUpdated:
		if isClosedOrMerged {
			var updated []github.PullRequest
			for _, p := range m.PRs {
				if isSamePR(p, pr) {
					continue
				}
				updated = append(updated, p)
			}
			m.PRs = updated
			if m.PRCursor >= len(m.PRs) && len(m.PRs) > 0 {
				m.PRCursor = len(m.PRs) - 1
			}
			stateWord := "closed"
			if strings.EqualFold(pr.State, "merged") {
				stateWord = "merged"
			}
			m.recordEntityFlash(fmt.Sprintf("pr:%s#%d", pr.DisplayRepo(), pr.Number))
			m.recordEntityFlash(fmt.Sprintf("pr:%s#%d", pr.Repo, pr.Number))
			m.StatusMsg = fmt.Sprintf("⚡ [PR] #%d %s in %s", pr.Number, stateWord, pr.DisplayRepo())
			m.StatusMsgTimer = time.Now().Add(3 * time.Second)
		} else {
			found := false
			for i, p := range m.PRs {
				if isSamePR(p, pr) {
					m.PRs[i] = pr
					found = true
					break
				}
			}
			if !found {
				m.PRs = append([]github.PullRequest{pr}, m.PRs...)
			}
			if env.Action == protocol.ActionCreated || !found {
				prKey := fmt.Sprintf("%s#%d", pr.DisplayRepo(), pr.Number)
				if m.SeenPRNotifications == nil {
					m.SeenPRNotifications = make(map[string]bool)
				}
				if !m.SeenPRNotifications[prKey] {
					m.SeenPRNotifications[prKey] = true
					SendPRNotification(pr)
				}
			}
			actionWord := "updated"
			if env.Action == protocol.ActionCreated {
				actionWord = "opened"
			}
			rev := ""
			if pr.ReviewDecision != "" {
				rev = fmt.Sprintf(" (%s)", pr.ReviewDecision)
			}
			m.recordEntityFlash(fmt.Sprintf("pr:%s#%d", pr.DisplayRepo(), pr.Number))
			m.recordEntityFlash(fmt.Sprintf("pr:%s#%d", pr.Repo, pr.Number))
			m.StatusMsg = fmt.Sprintf("⚡ [PR] #%d %s in %s%s", pr.Number, actionWord, pr.DisplayRepo(), rev)
			m.StatusMsgTimer = time.Now().Add(3 * time.Second)
		}
	}
	return m, nil
}

func isSamePR(a, b github.PullRequest) bool {
	if a.Number != b.Number {
		return false
	}
	if a.RepoFullName != "" && b.RepoFullName != "" {
		return strings.EqualFold(a.RepoFullName, b.RepoFullName)
	}
	return strings.EqualFold(a.Repo, b.Repo)
}

func (m Model) patchTask(env protocol.EventEnvelope) (Model, tea.Cmd) {
	var payload protocol.TaskPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		return m, nil
	}
	task := payload.Task
	isClosed := strings.EqualFold(task.State, "closed")

	switch env.Action {
	case protocol.ActionDeleted:
		m.PersonalTasks = removeTask(m.PersonalTasks, task)
		m.PersonalDoneTasks = removeTask(m.PersonalDoneTasks, task)
		m.SquadTasks = removeTask(m.SquadTasks, task)
		m.SquadDoneTasks = removeTask(m.SquadDoneTasks, task)
		m.StatusMsg = fmt.Sprintf("⚡ [Task] #%d deleted", task.Number)
		m.StatusMsgTimer = time.Now().Add(3 * time.Second)

	case protocol.ActionCreated, protocol.ActionUpdated:
		if isClosed {
			if task.IsSquadTask {
				m.SquadTasks = removeTask(m.SquadTasks, task)
				m.SquadDoneTasks = upsertTask(m.SquadDoneTasks, task)
			} else {
				m.PersonalTasks = removeTask(m.PersonalTasks, task)
				m.PersonalDoneTasks = upsertTask(m.PersonalDoneTasks, task)
			}
			if task.ID != 0 {
				m.recordEntityFlash(fmt.Sprintf("task:%d", task.ID))
			}
			m.recordEntityFlash(fmt.Sprintf("task:%s#%d", task.RepoName, task.Number))
			m.StatusMsg = fmt.Sprintf("⚡ [Task] #%d closed in %s", task.Number, task.RepoName)
		} else {
			if task.IsSquadTask {
				m.SquadDoneTasks = removeTask(m.SquadDoneTasks, task)
				m.SquadTasks = upsertTask(m.SquadTasks, task)
			} else {
				m.PersonalDoneTasks = removeTask(m.PersonalDoneTasks, task)
				m.PersonalTasks = upsertTask(m.PersonalTasks, task)
			}
			actionWord := "updated"
			if env.Action == protocol.ActionCreated {
				actionWord = "created"
			}
			if task.ID != 0 {
				m.recordEntityFlash(fmt.Sprintf("task:%d", task.ID))
			}
			m.recordEntityFlash(fmt.Sprintf("task:%s#%d", task.RepoName, task.Number))
			m.StatusMsg = fmt.Sprintf("⚡ [Task] #%d %s in %s", task.Number, actionWord, task.RepoName)
		}
		m.StatusMsgTimer = time.Now().Add(3 * time.Second)
	}
	return m, nil
}

func removeTask(tasks []github.Task, target github.Task) []github.Task {
	var res []github.Task
	for _, t := range tasks {
		if isSameTask(t, target) {
			continue
		}
		res = append(res, t)
	}
	return res
}

func upsertTask(tasks []github.Task, target github.Task) []github.Task {
	for i, t := range tasks {
		if isSameTask(t, target) {
			tasks[i] = target
			return tasks
		}
	}
	return append([]github.Task{target}, tasks...)
}

func isSameTask(a, b github.Task) bool {
	if a.ID != 0 && b.ID != 0 && a.ID == b.ID {
		return true
	}
	if a.Number == b.Number && strings.EqualFold(a.RepoName, b.RepoName) {
		if a.RepoOwner != "" && b.RepoOwner != "" {
			return strings.EqualFold(a.RepoOwner, b.RepoOwner)
		}
		return true
	}
	return false
}

func (m Model) patchSquadActivity(env protocol.EventEnvelope) (Model, tea.Cmd) {
	var payload protocol.SquadActivityPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		return m, nil
	}
	evt := payload.Event

	// Prepend to SquadEvents
	m.SquadEvents = append([]github.SquadEvent{evt}, m.SquadEvents...)
	if len(m.SquadEvents) > 500 {
		m.SquadEvents = m.SquadEvents[:500]
	}

	// If it's a commit event, also prepend to AllCommitEvents
	if strings.Contains(strings.ToLower(evt.Action), "commit") || strings.Contains(strings.ToLower(evt.Action), "push") {
		m.AllCommitEvents = append([]github.SquadEvent{evt}, m.AllCommitEvents...)
		if len(m.AllCommitEvents) > 500 {
			m.AllCommitEvents = m.AllCommitEvents[:500]
		}
	}

	m.StatusMsg = fmt.Sprintf("⚡ [Activity] %s: %s on %s", evt.Actor, evt.Action, evt.Repo)
	m.StatusMsgTimer = time.Now().Add(3 * time.Second)
	return m, nil
}
