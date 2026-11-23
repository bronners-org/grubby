package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDiscussionsGraphQL(t *testing.T) {
	now := time.Now()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			http.NotFound(w, r)
			return
		}

		var req struct {
			Query     string                 `json:"query"`
			Variables map[string]interface{} `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		if strings.Contains(req.Query, "discussionCategories") {
			resp := map[string]interface{}{
				"data": map[string]interface{}{
					"repository": map[string]interface{}{
						"id": "REPO_123",
						"discussionCategories": map[string]interface{}{
							"nodes": []map[string]interface{}{
								{
									"id":          "CAT_1",
									"name":        "General",
									"slug":        "general",
									"description": "General discussions",
								},
								{
									"id":          "CAT_2",
									"name":        "Platform",
									"slug":        "platform",
									"description": "Platform squad threads",
								},
							},
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		if strings.Contains(req.Query, "createDiscussion") {
			title, _ := req.Variables["title"].(string)
			body, _ := req.Variables["body"].(string)
			resp := map[string]interface{}{
				"data": map[string]interface{}{
					"createDiscussion": map[string]interface{}{
						"discussion": map[string]interface{}{
							"id":        "DISC_NEW",
							"number":    42,
							"title":     title,
							"body":      body,
							"createdAt": now.Format(time.RFC3339),
							"updatedAt": now.Format(time.RFC3339),
							"url":       "https://github.com/org/repo/discussions/42",
							"author": map[string]interface{}{
								"login": "simon",
							},
							"category": map[string]interface{}{
								"id":   "CAT_1",
								"name": "General",
								"slug": "general",
							},
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		if strings.Contains(req.Query, "updateDiscussion") {
			title, _ := req.Variables["title"].(string)
			resp := map[string]interface{}{
				"data": map[string]interface{}{
					"updateDiscussion": map[string]interface{}{
						"discussion": map[string]interface{}{
							"id":        "DISC_NEW",
							"number":    42,
							"title":     title,
							"body":      "Hello world",
							"updatedAt": now.Format(time.RFC3339),
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		if strings.Contains(req.Query, "addDiscussionComment") {
			body, _ := req.Variables["body"].(string)
			resp := map[string]interface{}{
				"data": map[string]interface{}{
					"addDiscussionComment": map[string]interface{}{
						"comment": map[string]interface{}{
							"id":        "CM_NEW",
							"body":      body,
							"createdAt": now.Format(time.RFC3339),
							"updatedAt": now.Format(time.RFC3339),
							"url":       "https://github.com/org/repo/discussions/42#comment-1",
							"author": map[string]interface{}{
								"login": "alice",
							},
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		if strings.Contains(req.Query, "... on Discussion") {
			// Fetch comments query
			resp := map[string]interface{}{
				"data": map[string]interface{}{
					"node": map[string]interface{}{
						"comments": map[string]interface{}{
							"totalCount": 1,
							"nodes": []map[string]interface{}{
								{
									"id":        "CM_1",
									"body":      "LGTM!",
									"createdAt": now.Format(time.RFC3339),
									"updatedAt": now.Format(time.RFC3339),
									"url":       "https://github.com/org/repo/discussions/1#c1",
									"author": map[string]interface{}{
										"login": "bob",
									},
								},
							},
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		// Fetch discussions
		resp := map[string]interface{}{
			"data": map[string]interface{}{
				"repository": map[string]interface{}{
					"discussions": map[string]interface{}{
						"totalCount": 1,
						"nodes": []map[string]interface{}{
							{
								"id":        "DISC_1",
								"number":    101,
								"title":     "Threads RFC",
								"body":      "Discussion on threads in grubby",
								"createdAt": now.Format(time.RFC3339),
								"updatedAt": now.Format(time.RFC3339),
								"url":       "https://github.com/org/repo/discussions/101",
								"author": map[string]interface{}{
									"login": "simon",
								},
								"category": map[string]interface{}{
									"id":   "CAT_2",
									"name": "Platform",
									"slug": "platform",
								},
								"labels": map[string]interface{}{
									"nodes": []map[string]interface{}{
										{"name": "squad:platform"},
									},
								},
								"comments": map[string]interface{}{
									"totalCount": 2,
									"nodes": []map[string]interface{}{
										{
											"id":        "CM_10",
											"body":      "First comment",
											"createdAt": now.Format(time.RFC3339),
											"updatedAt": now.Format(time.RFC3339),
											"author": map[string]interface{}{
												"login": "alice",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := &Client{
		token:      "dummy-token",
		httpClient: server.Client(),
		apiBaseURL: server.URL,
	}

	ctx := context.Background()

	// 1. Test GetDiscussionCategories
	repoID, cats, err := client.GetDiscussionCategories(ctx, "org", "repo")
	if err != nil {
		t.Fatalf("GetDiscussionCategories failed: %v", err)
	}
	if repoID != "REPO_123" {
		t.Errorf("expected repoID REPO_123, got %s", repoID)
	}
	if len(cats) != 2 || cats[0].Name != "General" {
		t.Errorf("unexpected categories: %+v", cats)
	}

	// 2. Test FetchDiscussions
	discs, err := client.FetchDiscussions(ctx, "org", "repo", "", 10)
	if err != nil {
		t.Fatalf("FetchDiscussions failed: %v", err)
	}
	if len(discs) != 1 {
		t.Fatalf("expected 1 discussion, got %d", len(discs))
	}
	if discs[0].Title != "Threads RFC" || discs[0].CommentsCount != 2 {
		t.Errorf("unexpected discussion fields: %+v", discs[0])
	}
	if len(discs[0].Comments) != 1 || discs[0].Comments[0].Author != "alice" {
		t.Errorf("unexpected comment in discussion: %+v", discs[0].Comments)
	}

	// 3. Test FetchDiscussionComments
	comments, err := client.FetchDiscussionComments(ctx, "DISC_1", 10)
	if err != nil {
		t.Fatalf("FetchDiscussionComments failed: %v", err)
	}
	if len(comments) != 1 || comments[0].Author != "bob" {
		t.Errorf("unexpected comments: %+v", comments)
	}

	// 4. Test CreateDiscussion
	newDisc, err := client.CreateDiscussion(ctx, "REPO_123", "CAT_1", "New Thread", "Hello world")
	if err != nil {
		t.Fatalf("CreateDiscussion failed: %v", err)
	}
	if newDisc.Number != 42 || newDisc.Title != "New Thread" {
		t.Errorf("unexpected new discussion: %+v", newDisc)
	}

	// 5. Test AddDiscussionComment
	newComment, err := client.AddDiscussionComment(ctx, "DISC_NEW", "Nice work!")
	if err != nil {
		t.Fatalf("AddDiscussionComment failed: %v", err)
	}
	if newComment.ID != "CM_NEW" || newComment.Author != "alice" {
		t.Errorf("unexpected new comment: %+v", newComment)
	}

	// 6. Test UpdateDiscussionTitle
	updatedDisc, err := client.UpdateDiscussionTitle(ctx, "DISC_NEW", "Renamed Thread")
	if err != nil {
		t.Fatalf("UpdateDiscussionTitle failed: %v", err)
	}
	if updatedDisc.Title != "Renamed Thread" {
		t.Errorf("expected updated title 'Renamed Thread', got %q", updatedDisc.Title)
	}

	// 7. Test FilterDiscussionsForSquad
	squadDiscs := []Discussion{
		{Title: "Platform topic", Category: DiscussionCategory{Name: "Platform", Slug: "platform"}},
		{Title: "Core topic", Labels: []string{"squad:core"}},
		{Title: "Backend topic", Category: DiscussionCategory{Name: "Backend", Slug: "backend"}},
		{Title: "General announcement", Category: DiscussionCategory{Name: "Announcements", Slug: "announcements"}},
	}
	platformOnly := FilterDiscussionsForSquad(squadDiscs, "platform", "core", "backend")
	if len(platformOnly) != 2 {
		t.Fatalf("expected 2 discussions for platform (own + general), got %d: %v", len(platformOnly), platformOnly)
	}
	if platformOnly[0].Title != "Platform topic" || platformOnly[1].Title != "General announcement" {
		t.Errorf("unexpected discussions for platform: %v", platformOnly)
	}

	coreOnly := FilterDiscussionsForSquad(squadDiscs, "core", "platform", "backend")
	if len(coreOnly) != 2 {
		t.Fatalf("expected 2 discussions for core (own + general), got %d: %v", len(coreOnly), coreOnly)
	}
	if coreOnly[0].Title != "Core topic" || coreOnly[1].Title != "General announcement" {
		t.Errorf("unexpected discussions for core: %v", coreOnly)
	}
}

func TestEnsureDiscussionsEnabled(t *testing.T) {
	patchCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" && r.URL.Path == "/graphql" {
			var req struct {
				Query string `json:"query"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if strings.Contains(req.Query, "hasDiscussionsEnabled") {
				// Return false to test PATCH call
				resp := map[string]interface{}{
					"data": map[string]interface{}{
						"repository": map[string]interface{}{
							"id":                    "REPO_123",
							"hasDiscussionsEnabled": false,
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
				return
			}
		}
		if r.Method == "PATCH" && r.URL.Path == "/repos/org/repo" {
			patchCalled = true
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["has_discussions"] != true {
				t.Errorf("expected has_discussions: true in PATCH body, got %v", body)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"has_discussions": true}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := &Client{
		token:      "dummy-token",
		httpClient: server.Client(),
		apiBaseURL: server.URL,
	}

	ok, err := client.EnsureDiscussionsEnabled(context.Background(), "org", "repo")
	if err != nil {
		t.Fatalf("EnsureDiscussionsEnabled failed: %v", err)
	}
	if !ok {
		t.Errorf("expected ok=true")
	}
	if !patchCalled {
		t.Errorf("expected PATCH /repos/org/repo to have been called")
	}
}
