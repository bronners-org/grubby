package ui

import (
	"math"
	"sort"
	"strings"
	"time"

	"grubby/internal/github"
)

type ThreadTier int

const (
	ThreadTierHot ThreadTier = iota
	ThreadTierWarm
	ThreadTierCold
	ThreadTierDead

	// Backward compatibility aliases
	ThreadTierActive = ThreadTierHot
	ThreadTierStale  = ThreadTierCold
)

func (t ThreadTier) String() string {
	switch t {
	case ThreadTierHot:
		return "Hot"
	case ThreadTierWarm:
		return "Warm"
	case ThreadTierCold:
		return "Cold"
	case ThreadTierDead:
		return "Dead"
	default:
		return "Hot"
	}
}

func (t ThreadTier) Icon() string {
	switch t {
	case ThreadTierHot:
		return "🔥"
	case ThreadTierWarm:
		return "🌞"
	case ThreadTierCold:
		return "🧊"
	case ThreadTierDead:
		return "💀"
	default:
		return "🔥"
	}
}

type ScoredDiscussion struct {
	Discussion github.Discussion
	Score      float64
	Tier       ThreadTier
	IsUnread   bool
	HasMention bool
}

// CalculateDiscussionScore computes the ranking score and assigns the discussion to a tier.
func CalculateDiscussionScore(d github.Discussion, currentUser string, now time.Time) ScoredDiscussion {
	if now.IsZero() {
		now = time.Now()
	}

	timeSinceUpdate := now.Sub(d.UpdatedAt)
	hours := timeSinceUpdate.Hours()
	if hours < 0 {
		hours = 0
	}

	isUnread := d.LastReadAt.IsZero() || d.UpdatedAt.After(d.LastReadAt)
	hasMention := false
	if currentUser != "" {
		handle := "@" + strings.ToLower(currentUser)
		if strings.Contains(strings.ToLower(d.Body), handle) {
			hasMention = true
		}
		for _, cm := range d.Comments {
			if strings.Contains(strings.ToLower(cm.Body), handle) {
				hasMention = true
				break
			}
		}
	}

	// Base recency decay formula
	// Score = (Replies + 1) / (Hours + 1)^1.2
	score := float64(d.CommentsCount+1) / math.Pow(hours+1.0, 1.2)

	if isUnread {
		score += 50.0
	}
	if hasMention && isUnread {
		score += 200.0
	}

	// Tier determination (replicates Hot | Warm | Cold | Dead from repository UI):
	// Hot: has unread mention OR updated <= 48 hours (2 days)
	// Warm: updated <= 7 days (168 hours)
	// Cold: updated <= 30 days (720 hours)
	// Dead: updated > 30 days (720 hours)
	var tier ThreadTier
	if hasMention && isUnread {
		tier = ThreadTierHot
	} else if hours <= 48 {
		tier = ThreadTierHot
	} else if hours <= 168 {
		tier = ThreadTierWarm
	} else if hours <= 720 {
		tier = ThreadTierCold
	} else {
		tier = ThreadTierDead
	}

	return ScoredDiscussion{
		Discussion: d,
		Score:      score,
		Tier:       tier,
		IsUnread:   isUnread,
		HasMention: hasMention,
	}
}

// SortDiscussions sorts scored discussions by Tier (Active -> Stale -> Dead) and then descending by Score.
func SortDiscussions(discs []ScoredDiscussion) []ScoredDiscussion {
	sorted := make([]ScoredDiscussion, len(discs))
	copy(sorted, discs)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Tier != sorted[j].Tier {
			return sorted[i].Tier < sorted[j].Tier
		}
		return sorted[i].Score > sorted[j].Score
	})
	return sorted
}
