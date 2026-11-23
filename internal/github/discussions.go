package github

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DiscussionCategory represents a GitHub Discussion category.
type DiscussionCategory struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description,omitempty"`
}

// DiscussionComment represents a reply in a GitHub Discussion.
type DiscussionComment struct {
	ID           string    `json:"id"`
	DiscussionID string    `json:"discussion_id,omitempty"`
	Author       string    `json:"author"`
	Body         string    `json:"body"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	URL          string    `json:"url,omitempty"`
}

// Discussion represents a GitHub Discussion thread.
type Discussion struct {
	ID            string              `json:"id"`
	Number        int                 `json:"number"`
	RepoOwner     string              `json:"repo_owner"`
	RepoName      string              `json:"repo_name"`
	Title         string              `json:"title"`
	Body          string              `json:"body"`
	Author        string              `json:"author"`
	CreatedAt     time.Time           `json:"created_at"`
	UpdatedAt     time.Time           `json:"updated_at"`
	URL           string              `json:"url"`
	Category      DiscussionCategory  `json:"category"`
	Labels        []string            `json:"labels"`
	CommentsCount int                 `json:"comments_count"`
	Comments      []DiscussionComment `json:"comments,omitempty"`
	LastReadAt    time.Time           `json:"last_read_at,omitempty"`
}

// GetDiscussionCategories retrieves all categories enabled for discussions in a repo.
func (c *Client) GetDiscussionCategories(ctx context.Context, owner, repo string) (string, []DiscussionCategory, error) {
	query := `query($owner: String!, $name: String!) {
		repository(owner: $owner, name: $name) {
			id
			discussionCategories(first: 25) {
				nodes {
					id
					name
					slug
					description
				}
			}
		}
	}`

	var resp struct {
		Repository *struct {
			ID                   string `json:"id"`
			DiscussionCategories struct {
				Nodes []DiscussionCategory `json:"nodes"`
			} `json:"discussionCategories"`
		} `json:"repository"`
	}

	err := c.queryGraphQL(ctx, query, map[string]interface{}{
		"owner": owner,
		"name":  repo,
	}, &resp)
	if err != nil {
		return "", nil, err
	}
	if resp.Repository == nil {
		return "", nil, fmt.Errorf("repository %s/%s not found", owner, repo)
	}

	return resp.Repository.ID, resp.Repository.DiscussionCategories.Nodes, nil
}

// FetchDiscussions retrieves discussions from a repository ordered by UPDATED_AT desc.
func (c *Client) FetchDiscussions(ctx context.Context, owner, repo, categoryID string, limit int) ([]Discussion, error) {
	if limit <= 0 {
		limit = 30
	}

	query := `query($owner: String!, $name: String!, $categoryId: ID, $first: Int!) {
		repository(owner: $owner, name: $name) {
			discussions(first: $first, categoryId: $categoryId, orderBy: {field: UPDATED_AT, direction: DESC}) {
				totalCount
				nodes {
					id
					number
					title
					body
					createdAt
					updatedAt
					url
					author {
						login
					}
					category {
						id
						name
						slug
					}
					labels(first: 10) {
						nodes {
							name
						}
					}
					comments(first: 50) {
						totalCount
						nodes {
							id
							body
							createdAt
							updatedAt
							url
							author {
								login
							}
						}
					}
				}
			}
		}
	}`

	vars := map[string]interface{}{
		"owner": owner,
		"name":  repo,
		"first": limit,
	}
	if categoryID != "" {
		vars["categoryId"] = categoryID
	}

	var resp struct {
		Repository *struct {
			Discussions struct {
				Nodes []struct {
					ID        string    `json:"id"`
					Number    int       `json:"number"`
					Title     string    `json:"title"`
					Body      string    `json:"body"`
					CreatedAt time.Time `json:"createdAt"`
					UpdatedAt time.Time `json:"updatedAt"`
					URL       string    `json:"url"`
					Author    *struct {
						Login string `json:"login"`
					} `json:"author"`
					Category struct {
						ID   string `json:"id"`
						Name string `json:"name"`
						Slug string `json:"slug"`
					} `json:"category"`
					Labels struct {
						Nodes []struct {
							Name string `json:"name"`
						} `json:"nodes"`
					} `json:"labels"`
					Comments struct {
						TotalCount int `json:"totalCount"`
						Nodes      []struct {
							ID        string    `json:"id"`
							Body      string    `json:"body"`
							CreatedAt time.Time `json:"createdAt"`
							UpdatedAt time.Time `json:"updatedAt"`
							URL       string    `json:"url"`
							Author    *struct {
								Login string `json:"login"`
							} `json:"author"`
						} `json:"nodes"`
					} `json:"comments"`
				} `json:"nodes"`
			} `json:"discussions"`
		} `json:"repository"`
	}

	if err := c.queryGraphQL(ctx, query, vars, &resp); err != nil {
		return nil, err
	}
	if resp.Repository == nil {
		return nil, fmt.Errorf("repository %s/%s not found", owner, repo)
	}

	var results []Discussion
	for _, n := range resp.Repository.Discussions.Nodes {
		author := "ghost"
		if n.Author != nil && n.Author.Login != "" {
			author = n.Author.Login
		}

		var labels []string
		for _, l := range n.Labels.Nodes {
			labels = append(labels, l.Name)
		}

		var comments []DiscussionComment
		for _, cm := range n.Comments.Nodes {
			cAuthor := "ghost"
			if cm.Author != nil && cm.Author.Login != "" {
				cAuthor = cm.Author.Login
			}
			comments = append(comments, DiscussionComment{
				ID:           cm.ID,
				DiscussionID: n.ID,
				Author:       cAuthor,
				Body:         cm.Body,
				CreatedAt:    cm.CreatedAt,
				UpdatedAt:    cm.UpdatedAt,
				URL:          cm.URL,
			})
		}

		results = append(results, Discussion{
			ID:            n.ID,
			Number:        n.Number,
			RepoOwner:     owner,
			RepoName:      repo,
			Title:         n.Title,
			Body:          n.Body,
			Author:        author,
			CreatedAt:     n.CreatedAt,
			UpdatedAt:     n.UpdatedAt,
			URL:           n.URL,
			Category:      DiscussionCategory{ID: n.Category.ID, Name: n.Category.Name, Slug: n.Category.Slug},
			Labels:        labels,
			CommentsCount: n.Comments.TotalCount,
			Comments:      comments,
		})
	}

	return results, nil
}

// FetchDiscussionComments retrieves comments for a specific discussion.
func (c *Client) FetchDiscussionComments(ctx context.Context, discussionID string, limit int) ([]DiscussionComment, error) {
	if limit <= 0 {
		limit = 50
	}

	query := `query($id: ID!, $first: Int!) {
		node(id: $id) {
			... on Discussion {
				comments(first: $first) {
					totalCount
					nodes {
						id
						body
						createdAt
						updatedAt
						url
						author {
							login
						}
					}
				}
			}
		}
	}`

	var resp struct {
		Node *struct {
			Comments struct {
				Nodes []struct {
					ID        string    `json:"id"`
					Body      string    `json:"body"`
					CreatedAt time.Time `json:"createdAt"`
					UpdatedAt time.Time `json:"updatedAt"`
					URL       string    `json:"url"`
					Author    *struct {
						Login string `json:"login"`
					} `json:"author"`
				} `json:"nodes"`
			} `json:"comments"`
		} `json:"node"`
	}

	if err := c.queryGraphQL(ctx, query, map[string]interface{}{
		"id":    discussionID,
		"first": limit,
	}, &resp); err != nil {
		return nil, err
	}
	if resp.Node == nil {
		return nil, fmt.Errorf("discussion %s not found", discussionID)
	}

	var comments []DiscussionComment
	for _, cm := range resp.Node.Comments.Nodes {
		author := "ghost"
		if cm.Author != nil && cm.Author.Login != "" {
			author = cm.Author.Login
		}
		comments = append(comments, DiscussionComment{
			ID:           cm.ID,
			DiscussionID: discussionID,
			Author:       author,
			Body:         cm.Body,
			CreatedAt:    cm.CreatedAt,
			UpdatedAt:    cm.UpdatedAt,
			URL:          cm.URL,
		})
	}
	return comments, nil
}

// CreateDiscussion creates a new discussion thread in GitHub.
func (c *Client) CreateDiscussion(ctx context.Context, repoID, categoryID, title, body string) (*Discussion, error) {
	mutation := `mutation($repositoryId: ID!, $categoryId: ID!, $title: String!, $body: String!) {
		createDiscussion(input: {repositoryId: $repositoryId, categoryId: $categoryId, title: $title, body: $body}) {
			discussion {
				id
				number
				title
				body
				createdAt
				updatedAt
				url
				author {
					login
				}
				category {
					id
					name
					slug
				}
			}
		}
	}`

	var resp struct {
		CreateDiscussion struct {
			Discussion struct {
				ID        string    `json:"id"`
				Number    int       `json:"number"`
				Title     string    `json:"title"`
				Body      string    `json:"body"`
				CreatedAt time.Time `json:"createdAt"`
				UpdatedAt time.Time `json:"updatedAt"`
				URL       string    `json:"url"`
				Author    *struct {
					Login string `json:"login"`
				} `json:"author"`
				Category struct {
					ID   string `json:"id"`
					Name string `json:"name"`
					Slug string `json:"slug"`
				} `json:"category"`
			} `json:"discussion"`
		} `json:"createDiscussion"`
	}

	vars := map[string]interface{}{
		"repositoryId": repoID,
		"categoryId":   categoryID,
		"title":        title,
		"body":         body,
	}

	if err := c.queryGraphQL(ctx, mutation, vars, &resp); err != nil {
		return nil, err
	}

	d := resp.CreateDiscussion.Discussion
	author := "ghost"
	if d.Author != nil && d.Author.Login != "" {
		author = d.Author.Login
	}

	return &Discussion{
		ID:        d.ID,
		Number:    d.Number,
		Title:     d.Title,
		Body:      d.Body,
		Author:    author,
		CreatedAt: d.CreatedAt,
		UpdatedAt: d.UpdatedAt,
		URL:       d.URL,
		Category:  DiscussionCategory{ID: d.Category.ID, Name: d.Category.Name, Slug: d.Category.Slug},
	}, nil
}

// AddDiscussionComment adds a reply to an existing discussion thread.
func (c *Client) AddDiscussionComment(ctx context.Context, discussionID, body string) (*DiscussionComment, error) {
	mutation := `mutation($discussionId: ID!, $body: String!) {
		addDiscussionComment(input: {discussionId: $discussionId, body: $body}) {
			comment {
				id
				body
				createdAt
				updatedAt
				url
				author {
					login
				}
			}
		}
	}`

	var resp struct {
		AddDiscussionComment struct {
			Comment struct {
				ID        string    `json:"id"`
				Body      string    `json:"body"`
				CreatedAt time.Time `json:"createdAt"`
				UpdatedAt time.Time `json:"updatedAt"`
				URL       string    `json:"url"`
				Author    *struct {
					Login string `json:"login"`
				} `json:"author"`
			} `json:"comment"`
		} `json:"addDiscussionComment"`
	}

	vars := map[string]interface{}{
		"discussionId": discussionID,
		"body":         body,
	}

	if err := c.queryGraphQL(ctx, mutation, vars, &resp); err != nil {
		return nil, err
	}

	cm := resp.AddDiscussionComment.Comment
	author := "ghost"
	if cm.Author != nil && cm.Author.Login != "" {
		author = cm.Author.Login
	}

	return &DiscussionComment{
		ID:           cm.ID,
		DiscussionID: discussionID,
		Author:       author,
		Body:         cm.Body,
		CreatedAt:    cm.CreatedAt,
		UpdatedAt:    cm.UpdatedAt,
		URL:          cm.URL,
	}, nil
}

// UpdateDiscussionTitle updates the title of a discussion thread in GitHub.
func (c *Client) UpdateDiscussionTitle(ctx context.Context, discussionID, newTitle string) (*Discussion, error) {
	mutation := `mutation($discussionId: ID!, $title: String!) {
		updateDiscussion(input: {discussionId: $discussionId, title: $title}) {
			discussion {
				id
				number
				title
				body
				updatedAt
			}
		}
	}`

	var resp struct {
		UpdateDiscussion struct {
			Discussion struct {
				ID        string    `json:"id"`
				Number    int       `json:"number"`
				Title     string    `json:"title"`
				Body      string    `json:"body"`
				UpdatedAt time.Time `json:"updatedAt"`
			} `json:"discussion"`
		} `json:"updateDiscussion"`
	}

	vars := map[string]interface{}{
		"discussionId": discussionID,
		"title":        newTitle,
	}

	if err := c.queryGraphQL(ctx, mutation, vars, &resp); err != nil {
		return nil, err
	}

	d := resp.UpdateDiscussion.Discussion
	return &Discussion{
		ID:        d.ID,
		Number:    d.Number,
		Title:     d.Title,
		Body:      d.Body,
		UpdatedAt: d.UpdatedAt,
	}, nil
}

// UpdateDiscussionBody updates the body content of a discussion thread in GitHub.
func (c *Client) UpdateDiscussionBody(ctx context.Context, discussionID, newBody string) (*Discussion, error) {
	mutation := `mutation($discussionId: ID!, $body: String!) {
		updateDiscussion(input: {discussionId: $discussionId, body: $body}) {
			discussion {
				id
				number
				title
				body
				updatedAt
			}
		}
	}`

	var resp struct {
		UpdateDiscussion struct {
			Discussion struct {
				ID        string    `json:"id"`
				Number    int       `json:"number"`
				Title     string    `json:"title"`
				Body      string    `json:"body"`
				UpdatedAt time.Time `json:"updatedAt"`
			} `json:"discussion"`
		} `json:"updateDiscussion"`
	}

	vars := map[string]interface{}{
		"discussionId": discussionID,
		"body":         newBody,
	}

	if err := c.queryGraphQL(ctx, mutation, vars, &resp); err != nil {
		return nil, err
	}

	d := resp.UpdateDiscussion.Discussion
	return &Discussion{
		ID:        d.ID,
		Number:    d.Number,
		Title:     d.Title,
		Body:      d.Body,
		UpdatedAt: d.UpdatedAt,
	}, nil
}

// UpdateDiscussionComment updates the body of a discussion comment in GitHub.
func (c *Client) UpdateDiscussionComment(ctx context.Context, commentID, newBody string) (*DiscussionComment, error) {
	mutation := `mutation($commentId: ID!, $body: String!) {
		updateDiscussionComment(input: {commentId: $commentId, body: $body}) {
			comment {
				id
				body
				updatedAt
			}
		}
	}`

	var resp struct {
		UpdateDiscussionComment struct {
			Comment struct {
				ID        string    `json:"id"`
				Body      string    `json:"body"`
				UpdatedAt time.Time `json:"updatedAt"`
			} `json:"comment"`
		} `json:"updateDiscussionComment"`
	}

	vars := map[string]interface{}{
		"commentId": commentID,
		"body":      newBody,
	}

	if err := c.queryGraphQL(ctx, mutation, vars, &resp); err != nil {
		return nil, err
	}

	cm := resp.UpdateDiscussionComment.Comment
	return &DiscussionComment{
		ID:        cm.ID,
		Body:      cm.Body,
		UpdatedAt: cm.UpdatedAt,
	}, nil
}

// EnsureDiscussionsEnabled checks if discussions are enabled on a repository, and if not, enables them via PATCH /repos/{owner}/{repo}.
func (c *Client) EnsureDiscussionsEnabled(ctx context.Context, owner, repo string) (bool, error) {
	if owner == "" || repo == "" {
		return false, fmt.Errorf("owner and repo must not be empty")
	}

	// 1. Check if already enabled
	query := `query($owner: String!, $name: String!) {
		repository(owner: $owner, name: $name) {
			id
			hasDiscussionsEnabled
		}
	}`

	var resp struct {
		Repository *struct {
			ID                    string `json:"id"`
			HasDiscussionsEnabled bool   `json:"hasDiscussionsEnabled"`
		} `json:"repository"`
	}

	err := c.queryGraphQL(ctx, query, map[string]interface{}{
		"owner": owner,
		"name":  repo,
	}, &resp)
	if err == nil && resp.Repository != nil && resp.Repository.HasDiscussionsEnabled {
		return true, nil
	}

	// 2. Enable via REST API PATCH /repos/{owner}/{repo}
	patchURL := fmt.Sprintf("%s/repos/%s/%s", c.apiBaseURLOrDefault(), owner, repo)
	bodyBytes := []byte(`{"has_discussions":true}`)

	req, err := http.NewRequestWithContext(ctx, "PATCH", patchURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := c.do(req)
	if err != nil {
		return false, err
	}
	defer httpResp.Body.Close()
	c.updateRateLimits(httpResp)

	if httpResp.StatusCode >= 200 && httpResp.StatusCode < 300 {
		return true, nil
	}

	respBody, _ := io.ReadAll(httpResp.Body)
	return false, fmt.Errorf("failed to enable discussions (status %d): %s", httpResp.StatusCode, string(respBody))
}

// FilterDiscussionsForSquad filters a list of discussions for a given squad.
// Discussions explicitly matching the squad (via category or squad:<name> label) are included.
// Discussions explicitly belonging to other squads are excluded.
// General/unscoped discussions (e.g. Announcements, General, Q&A) are included by default.
func FilterDiscussionsForSquad(discussions []Discussion, squad string, otherSquads ...string) []Discussion {
	if strings.TrimSpace(squad) == "" {
		return discussions
	}
	squadLower := strings.ToLower(strings.TrimSpace(squad))
	targetLabel := "squad:" + squadLower

	otherSquadMap := make(map[string]bool)
	for _, o := range otherSquads {
		ol := strings.ToLower(strings.TrimSpace(o))
		if ol != "" && ol != squadLower {
			otherSquadMap[ol] = true
			otherSquadMap["squad:"+ol] = true
		}
	}

	var filtered []Discussion
	for _, d := range discussions {
		catLower := strings.ToLower(strings.TrimSpace(d.Category.Name))
		slugLower := strings.ToLower(strings.TrimSpace(d.Category.Slug))

		// 1. Explicit match for current squad
		if catLower == squadLower || slugLower == squadLower {
			filtered = append(filtered, d)
			continue
		}
		explicitMatch := false
		for _, l := range d.Labels {
			ll := strings.ToLower(strings.TrimSpace(l))
			if ll == targetLabel || ll == squadLower {
				explicitMatch = true
				break
			}
		}
		if explicitMatch {
			filtered = append(filtered, d)
			continue
		}

		// 2. Check if explicitly belonging to another squad
		belongsToOther := false
		if otherSquadMap[catLower] || otherSquadMap[slugLower] {
			belongsToOther = true
		} else {
			for _, l := range d.Labels {
				ll := strings.ToLower(strings.TrimSpace(l))
				if strings.HasPrefix(ll, "squad:") && ll != targetLabel {
					belongsToOther = true
					break
				}
				if otherSquadMap[ll] {
					belongsToOther = true
					break
				}
			}
		}
		if belongsToOther {
			continue // Exclude discussions belonging to another squad
		}

		// 3. Unscoped / General discussion (e.g. Announcements, General, Q&A, etc.)
		filtered = append(filtered, d)
	}
	return filtered
}
