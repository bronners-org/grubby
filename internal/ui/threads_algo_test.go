package ui

import (
	"testing"
	"time"

	"grubby/internal/github"
)

func TestExtractMentions(t *testing.T) {
	text := "Hey @alice, check out #grubby and pull request !15 or #42. Also tell @bob about #tasks!"
	mentions := ExtractMentions(text)

	var users, repos, prs []string
	for _, m := range mentions {
		switch m.Type {
		case MentionUser:
			users = append(users, m.Value)
		case MentionRepo:
			repos = append(repos, m.Value)
		case MentionPR:
			prs = append(prs, m.Value)
		}
	}

	if len(users) != 2 || users[0] != "alice" || users[1] != "bob" {
		t.Errorf("unexpected users: %v", users)
	}
	if len(repos) != 2 || repos[0] != "grubby" || repos[1] != "tasks" {
		t.Errorf("unexpected repos: %v", repos)
	}
	if len(prs) != 2 || prs[0] != "15" || prs[1] != "42" {
		t.Errorf("unexpected prs: %v", prs)
	}
}

func TestCalculateDiscussionScore(t *testing.T) {
	now := time.Now()

	// Hot thread: updated 1h ago
	d1 := github.Discussion{
		ID:            "D_1",
		Title:         "Hot Discussion",
		UpdatedAt:     now.Add(-1 * time.Hour),
		CommentsCount: 5,
		LastReadAt:    now,
	}
	s1 := CalculateDiscussionScore(d1, "simon", now)
	if s1.Tier != ThreadTierHot {
		t.Errorf("expected TierHot for 1h ago, got %v", s1.Tier)
	}

	// Warm thread: updated 3 days ago (72h)
	d2 := github.Discussion{
		ID:            "D_2",
		Title:         "Warm Discussion",
		UpdatedAt:     now.Add(-72 * time.Hour),
		CommentsCount: 2,
		LastReadAt:    now,
	}
	s2 := CalculateDiscussionScore(d2, "simon", now)
	if s2.Tier != ThreadTierWarm {
		t.Errorf("expected TierWarm for 72h ago, got %v", s2.Tier)
	}

	// Cold thread: updated 10 days ago (240h)
	d3 := github.Discussion{
		ID:            "D_3",
		Title:         "Cold Discussion",
		UpdatedAt:     now.Add(-240 * time.Hour),
		CommentsCount: 0,
		LastReadAt:    now,
	}
	s3 := CalculateDiscussionScore(d3, "simon", now)
	if s3.Tier != ThreadTierCold {
		t.Errorf("expected TierCold for 240h ago, got %v", s3.Tier)
	}

	// Dead thread: updated 40 days ago (960h)
	dDead := github.Discussion{
		ID:            "D_Dead",
		Title:         "Dead Discussion",
		UpdatedAt:     now.Add(-960 * time.Hour),
		CommentsCount: 0,
		LastReadAt:    now,
	}
	sDead := CalculateDiscussionScore(dDead, "simon", now)
	if sDead.Tier != ThreadTierDead {
		t.Errorf("expected TierDead for 960h ago, got %v", sDead.Tier)
	}

	// Thread with unread mention: jumps to Hot tier regardless of age!
	d4 := github.Discussion{
		ID:            "D_4",
		Title:         "Old Mentioned Discussion",
		Body:          "Pinging @simon on this",
		UpdatedAt:     now.Add(-1000 * time.Hour),
		CommentsCount: 1,
		LastReadAt:    now.Add(-1050 * time.Hour), // Unread!
	}
	s4 := CalculateDiscussionScore(d4, "simon", now)
	if s4.Tier != ThreadTierHot {
		t.Errorf("expected TierHot for unread mention, got %v", s4.Tier)
	}
	if !s4.HasMention || !s4.IsUnread {
		t.Errorf("expected HasMention and IsUnread true, got mention=%v unread=%v", s4.HasMention, s4.IsUnread)
	}

	// Sorting: Hot > Warm > Cold > Dead
	sorted := SortDiscussions([]ScoredDiscussion{sDead, s3, s2, s1, s4})
	if sorted[0].Tier != ThreadTierHot || sorted[len(sorted)-1].Tier != ThreadTierDead {
		t.Errorf("unexpected sort order: %+v", sorted)
	}
}
