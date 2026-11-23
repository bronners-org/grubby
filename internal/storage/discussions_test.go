package storage

import (
	"testing"
	"time"

	"grubby/internal/github"
)

func TestStorage_Discussions(t *testing.T) {
	db := setupTestDB(t)

	now := time.Now().Truncate(time.Second)

	disc1 := github.Discussion{
		ID:            "D_1",
		Number:        1,
		RepoOwner:     "org",
		RepoName:      "tasks",
		Title:         "Discussion One",
		Body:          "Hello world",
		Author:        "alice",
		CreatedAt:     now.Add(-2 * time.Hour),
		UpdatedAt:     now.Add(-1 * time.Hour),
		URL:           "https://github.com/org/tasks/discussions/1",
		Category:      github.DiscussionCategory{ID: "CAT_1", Name: "Platform", Slug: "platform"},
		CommentsCount: 1,
		Comments: []github.DiscussionComment{
			{
				ID:           "DC_1",
				DiscussionID: "D_1",
				Author:       "bob",
				Body:         "Reply one",
				CreatedAt:    now.Add(-1 * time.Hour),
				UpdatedAt:    now.Add(-1 * time.Hour),
			},
		},
	}

	disc2 := github.Discussion{
		ID:            "D_2",
		Number:        2,
		RepoOwner:     "org",
		RepoName:      "tasks",
		Title:         "Discussion Two",
		Body:          "Second thread",
		Author:        "charlie",
		CreatedAt:     now.Add(-5 * time.Hour),
		UpdatedAt:     now.Add(-3 * time.Hour),
		URL:           "https://github.com/org/tasks/discussions/2",
		Category:      github.DiscussionCategory{ID: "CAT_2", Name: "Core", Slug: "core"},
		CommentsCount: 0,
	}

	// 1. Save discussions
	err := db.SaveDiscussions([]github.Discussion{disc1, disc2}, "platform")
	if err != nil {
		t.Fatalf("SaveDiscussions failed: %v", err)
	}

	// 2. Query discussions
	discs, err := db.GetDiscussions("")
	if err != nil {
		t.Fatalf("GetDiscussions failed: %v", err)
	}
	if len(discs) != 2 {
		t.Fatalf("expected 2 discussions, got %d", len(discs))
	}

	// 3. Query comments
	comments, err := db.GetDiscussionComments("D_1")
	if err != nil {
		t.Fatalf("GetDiscussionComments failed: %v", err)
	}
	if len(comments) != 1 || comments[0].Author != "bob" {
		t.Fatalf("unexpected comments: %+v", comments)
	}

	// 4. Save new comment
	cm2 := github.DiscussionComment{
		ID:           "DC_2",
		DiscussionID: "D_1",
		Author:       "david",
		Body:         "Reply two",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	err = db.SaveDiscussionComment(cm2)
	if err != nil {
		t.Fatalf("SaveDiscussionComment failed: %v", err)
	}

	updatedDisc, err := db.GetDiscussion("D_1")
	if err != nil {
		t.Fatalf("GetDiscussion failed: %v", err)
	}
	if updatedDisc.CommentsCount != 2 {
		t.Errorf("expected comments_count=2, got %d", updatedDisc.CommentsCount)
	}

	// 5. Unread count and mark read
	unread, err := db.GetUnreadDiscussionsCount("platform")
	if err != nil {
		t.Fatalf("GetUnreadDiscussionsCount failed: %v", err)
	}
	if unread != 2 {
		t.Errorf("expected 2 unread discussions, got %d", unread)
	}

	err = db.MarkDiscussionRead("D_1")
	if err != nil {
		t.Fatalf("MarkDiscussionRead failed: %v", err)
	}

	unreadAfter, err := db.GetUnreadDiscussionsCount("platform")
	if err != nil {
		t.Fatalf("GetUnreadDiscussionsCount failed: %v", err)
	}
	if unreadAfter != 1 {
		t.Errorf("expected 1 unread discussion after mark read, got %d", unreadAfter)
	}
}

func TestStorage_DiscussionMessageEditAndSeenTracking(t *testing.T) {
	db := setupTestDB(t)
	now := time.Now().Truncate(time.Second)

	disc := github.Discussion{
		ID:            "D_EDIT_1",
		Number:        10,
		RepoOwner:     "org",
		RepoName:      "tasks",
		Title:         "Original Title",
		Body:          "Original discussion body",
		Author:        "alice",
		CreatedAt:     now.Add(-1 * time.Hour),
		UpdatedAt:     now.Add(-1 * time.Hour),
		URL:           "https://github.com/org/tasks/discussions/10",
		Category:      github.DiscussionCategory{ID: "CAT_1", Name: "Platform", Slug: "platform"},
		CommentsCount: 1,
		Comments: []github.DiscussionComment{
			{
				ID:           "DC_EDIT_1",
				DiscussionID: "D_EDIT_1",
				Author:       "bob",
				Body:         "Original comment body",
				CreatedAt:    now.Add(-30 * time.Minute),
				UpdatedAt:    now.Add(-30 * time.Minute),
			},
		},
	}

	if err := db.SaveDiscussions([]github.Discussion{disc}, "platform"); err != nil {
		t.Fatalf("SaveDiscussions failed: %v", err)
	}

	// 1. Update discussion body
	newDiscBody := "Updated discussion body with more details"
	if err := db.UpdateDiscussionBody("D_EDIT_1", newDiscBody); err != nil {
		t.Fatalf("UpdateDiscussionBody failed: %v", err)
	}
	fetchedDisc, err := db.GetDiscussion("D_EDIT_1")
	if err != nil {
		t.Fatalf("GetDiscussion failed: %v", err)
	}
	if fetchedDisc.Body != newDiscBody {
		t.Errorf("expected updated body %q, got %q", newDiscBody, fetchedDisc.Body)
	}

	// 2. Update comment body
	newCmBody := "Updated comment body with revision"
	if err := db.UpdateDiscussionCommentBody("DC_EDIT_1", newCmBody); err != nil {
		t.Fatalf("UpdateDiscussionCommentBody failed: %v", err)
	}
	comments, err := db.GetDiscussionComments("D_EDIT_1")
	if err != nil {
		t.Fatalf("GetDiscussionComments failed: %v", err)
	}
	if len(comments) != 1 || comments[0].Body != newCmBody {
		t.Errorf("expected updated comment body %q, got %+v", newCmBody, comments)
	}

	// 3. Seen messages tracking
	seenInitial, err := db.GetSeenMessageIDs()
	if err != nil {
		t.Fatalf("GetSeenMessageIDs failed: %v", err)
	}
	if seenInitial["D_EDIT_1"] || seenInitial["DC_EDIT_1"] {
		t.Errorf("expected message IDs not to be marked seen initially")
	}

	// Mark both as seen
	if err := db.MarkMessagesSeen([]string{"D_EDIT_1", "DC_EDIT_1"}, "D_EDIT_1"); err != nil {
		t.Fatalf("MarkMessagesSeen failed: %v", err)
	}

	seenAfter, err := db.GetSeenMessageIDs()
	if err != nil {
		t.Fatalf("GetSeenMessageIDs failed: %v", err)
	}
	if !seenAfter["D_EDIT_1"] || !seenAfter["DC_EDIT_1"] {
		t.Errorf("expected both message IDs marked seen, got map: %+v", seenAfter)
	}

	// 4. Idempotency of MarkMessagesSeen (ON CONFLICT DO NOTHING)
	if err := db.MarkMessagesSeen([]string{"D_EDIT_1", "DC_EDIT_1", ""}, "D_EDIT_1"); err != nil {
		t.Fatalf("subsequent MarkMessagesSeen failed: %v", err)
	}
}
