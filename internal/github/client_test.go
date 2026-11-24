package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseEvents(t *testing.T) {
	samplePushEvent := `{
		"type": "PushEvent",
		"actor": {"login": "simonbronner"},
		"repo": {"name": "simonbronner/seqta-agent-cf"},
		"payload": {
			"ref": "refs/heads/main",
			"commits": [{"message": "feat: improve notification formatting\n\nFull details"}]
		},
		"created_at": "2026-10-17T01:22:21Z"
	}`

	var raw struct {
		Type      string          `json:"type"`
		CreatedAt time.Time       `json:"created_at"`
		Actor     struct {
			Login string `json:"login"`
		} `json:"actor"`
		Repo struct {
			Name string `json:"name"`
		} `json:"repo"`
		Payload json.RawMessage `json:"payload"`
	}

	err := json.Unmarshal([]byte(samplePushEvent), &raw)
	if err != nil {
		t.Fatalf("failed to parse event json: %v", err)
	}

	if raw.Type != "PushEvent" {
		t.Errorf("expected PushEvent, got %s", raw.Type)
	}

	var p struct {
		Ref     string `json:"ref"`
		Commits []struct {
			Message string `json:"message"`
		} `json:"commits"`
	}
	_ = json.Unmarshal(raw.Payload, &p)

	branch := strings.TrimPrefix(p.Ref, "refs/heads/")
	if branch != "main" {
		t.Errorf("expected branch main, got %s", branch)
	}

	if len(p.Commits) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(p.Commits))
	}

	firstLine := strings.Split(p.Commits[0].Message, "\n")[0]
	if firstLine != "feat: improve notification formatting" {
		t.Errorf("unexpected commit message: %s", firstLine)
	}
}

func TestProxyResolution(t *testing.T) {
	// Test normalizeProxyURL
	u, err := normalizeProxyURL("http://proxy.corp.com:8080")
	if err != nil || u.Host != "proxy.corp.com:8080" || u.Scheme != "http" {
		t.Errorf("normalizeProxyURL failed for http: %v, %v", u, err)
	}

	u, err = normalizeProxyURL("socks5://127.0.0.1:1080")
	if err != nil || u.Host != "127.0.0.1:1080" || u.Scheme != "socks5" {
		t.Errorf("normalizeProxyURL failed for socks5: %v, %v", u, err)
	}

	u, err = normalizeProxyURL("proxy.internal:3128")
	if err != nil || u.Host != "proxy.internal:3128" || u.Scheme != "http" {
		t.Errorf("normalizeProxyURL failed for missing scheme: %v, %v", u, err)
	}

	u, err = normalizeProxyURL("")
	if err != nil || u != nil {
		t.Errorf("normalizeProxyURL failed for empty string: %v, %v", u, err)
	}

	// Test ResolveProxy priority
	explicit := "http://explicit-proxy:8080"
	if p := ResolveProxy(explicit); p != explicit {
		t.Errorf("expected explicit proxy %q, got %q", explicit, p)
	}

	// Test env var fallback
	t.Setenv("HTTPS_PROXY", "http://env-proxy:8443")
	if p := ResolveProxy(""); p != "http://env-proxy:8443" {
		t.Errorf("expected env proxy %q, got %q", "http://env-proxy:8443", p)
	}
}

func TestLiveFetchRepositoriesAndPRs(t *testing.T) {
	token, err := GetGHToken()
	if err != nil {
		t.Skip("gh auth not available")
	}

	client, err := NewClient(token)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	targetOrg := "bronners-org"
	repos, prs, commitEvents, secMap, err := client.FetchRepositoriesAndPRs(ctx, targetOrg)
	if err != nil {
		t.Fatalf("FetchRepositoriesAndPRs failed: %v", err)
	}

	if len(repos) == 0 {
		t.Fatalf("expected repos for %s, got 0", targetOrg)
	}

	// Verify that fetching for a user whose repos were transferred to an org (simonbronner)
	// returns 0 repos rather than misattributing the org's repos to the user.
	userRepos, _, _, _, err := client.FetchRepositoriesAndPRs(ctx, "simonbronner")
	if err != nil {
		t.Fatalf("FetchRepositoriesAndPRs failed for simonbronner: %v", err)
	}
	if len(userRepos) != 0 {
		t.Fatalf("expected 0 repos for simonbronner after transfer to bronners-org, got %d (e.g. %+v)", len(userRepos), userRepos[0])
	}

	// Verify open PRs are fetched properly
	if len(prs) == 0 {
		t.Logf("no open PRs returned, verifying PR structure")
	} else {
		for _, pr := range prs {
			if pr.Repo == "" || pr.Number <= 0 || pr.Title == "" {
				t.Errorf("invalid PR struct: %+v", pr)
			}
		}
	}

	// Verify commit events include grubby
	foundGrubbyCommit := false
	for _, ce := range commitEvents {
		if ce.Repo == "grubby" {
			foundGrubbyCommit = true
			break
		}
	}
	if !foundGrubbyCommit {
		t.Errorf("expected commit events for grubby")
	}

	// Verify squad activity includes recent activity
	squadEvents, err := client.FetchSquadActivity(ctx, []string{"simonbronner"}, commitEvents)
	if err != nil {
		t.Fatalf("FetchSquadActivity failed: %v", err)
	}
	if len(squadEvents) == 0 {
		t.Fatalf("expected squad events, got 0")
	}

	// Verify the most recent squad event has a valid timestamp
	mostRecent := squadEvents[0]
	if mostRecent.Timestamp.IsZero() {
		t.Errorf("expected most recent event to have a valid timestamp, got zero time")
	}

	// Verify security map includes best-resume-ever
	sec, ok := secMap["best-resume-ever"]
	if !ok || sec.Total == 0 {
		t.Errorf("expected security alerts for best-resume-ever, got: %+v", sec)
	}
}

func TestLiveFetchOrgMembers(t *testing.T) {
	token, err := GetGHToken()
	if err != nil {
		t.Skip("gh auth not available")
	}

	client, err := NewClient(token)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	members, err := client.FetchOrgMembers(ctx, "github")
	if err != nil {
		t.Fatalf("FetchOrgMembers failed: %v", err)
	}
	if len(members) == 0 {
		t.Fatalf("expected members for github org, got 0")
	}
	for _, m := range members {
		if m.Login == "" {
			t.Errorf("member has empty login: %+v", m)
		}
	}
}

func TestLiveFetchUserProfile(t *testing.T) {
	token, err := GetGHToken()
	if err != nil {
		t.Skip("gh auth not available")
	}

	client, err := NewClient(token)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	prof, err := client.FetchUserProfile(ctx, "simonbronner")
	if err != nil {
		t.Fatalf("FetchUserProfile failed: %v", err)
	}
	if prof.Login != "simonbronner" {
		t.Errorf("expected login simonbronner, got %s", prof.Login)
	}
	if prof.FormattedID() == "" {
		t.Errorf("expected non-empty user ID")
	}
	if prof.AvatarURL == "" {
		t.Errorf("expected non-empty avatar URL")
	}
}

func TestLiveFetchUserProfiles(t *testing.T) {
	token, err := GetGHToken()
	if err != nil {
		t.Skip("gh auth not available")
	}

	client, err := NewClient(token)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	profs, err := client.FetchUserProfiles(ctx, []string{"simonbronner", "octocat"})
	if err != nil {
		t.Fatalf("FetchUserProfiles failed: %v", err)
	}
	if len(profs) < 2 {
		t.Fatalf("expected at least 2 profiles, got %d", len(profs))
	}
	if sb, ok := profs["simonbronner"]; !ok || sb.Login != "simonbronner" {
		t.Errorf("expected simonbronner in profiles, got %+v", profs)
	}
}

func TestNormalizeRepoName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"grubby", "grubby"},
		{"simonbronner/grubby", "grubby"},
		{"org/repo.git", "repo"},
		{"https://github.com/org/repo", "repo"},
		{"https://github.com/org/repo.git", "repo"},
		{"git@github.com:org/repo.git", "repo"},
		{"org/nested/repo", "repo"},
		{"", "unknown"},
		{"  org/repo  ", "repo"},
	}

	for _, tc := range tests {
		actual := NormalizeRepoName(tc.input)
		if actual != tc.expected {
			t.Errorf("NormalizeRepoName(%q) = %q; expected %q", tc.input, actual, tc.expected)
		}
	}
}

func TestRequestTracking(t *testing.T) {
	client, err := NewClient("dummy-token")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	client.SetInitialRequestStats(10, 25)
	if client.RequestsMade() != 10 {
		t.Errorf("expected 10 requests made, got %d", client.RequestsMade())
	}
	if client.RequestsSaved() != 25 {
		t.Errorf("expected 25 requests saved, got %d", client.RequestsSaved())
	}

	client.RecordRequestMade()
	if client.RequestsMade() != 11 {
		t.Errorf("expected 11 requests made, got %d", client.RequestsMade())
	}

	client.RecordRequestSaved(5)
	if client.RequestsSaved() != 30 {
		t.Errorf("expected 30 requests saved, got %d", client.RequestsSaved())
	}
}

func TestMergePullRequest(t *testing.T) {
	var requestedPath string
	var requestedMethod string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		requestedMethod = r.Method

		if strings.Contains(r.URL.Path, "fail") {
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, _ = w.Write([]byte(`{"message": "Pull Request is not mergeable"}`))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"sha": "abcdef123456", "merged": true, "message": "Pull Request successfully merged"}`))
	}))
	defer server.Close()

	client := &Client{
		token:      "dummy-token",
		httpClient: server.Client(),
		apiBaseURL: server.URL,
	}

	ctx := context.Background()

	// 1. Successful merge
	err := client.MergePullRequest(ctx, "my-org", "my-repo", 123)
	if err != nil {
		t.Fatalf("expected merge to succeed, got: %v", err)
	}
	if requestedMethod != "PUT" {
		t.Errorf("expected PUT method, got %s", requestedMethod)
	}
	if requestedPath != "/repos/my-org/my-repo/pulls/123/merge" {
		t.Errorf("expected path /repos/my-org/my-repo/pulls/123/merge, got %s", requestedPath)
	}

	// 2. Failed merge
	err = client.MergePullRequest(ctx, "my-org", "fail-repo", 456)
	if err == nil {
		t.Fatalf("expected merge error, got nil")
	}
	if !strings.Contains(err.Error(), "Pull Request is not mergeable") {
		t.Errorf("expected error message to contain 'Pull Request is not mergeable', got %v", err)
	}
}

func TestFetchSingleRepo(t *testing.T) {
	now := time.Now()
	mockGraphQL := `{
		"data": {
			"repository": {
				"databaseId": 12345,
				"name": "payment-api",
				"nameWithOwner": "corp-org/payment-api",
				"owner": {"login": "corp-org"},
				"description": "Payment microservice",
				"pushedAt": "` + now.Format(time.RFC3339) + `",
				"updatedAt": "` + now.Format(time.RFC3339) + `",
				"isPrivate": true,
				"url": "https://github.com/corp-org/payment-api",
				"sshUrl": "git@github.com:corp-org/payment-api.git",
				"defaultBranchRef": {
					"name": "main",
					"target": {
						"history": {
							"nodes": [
								{
									"oid": "commit123",
									"message": "fix: stripe webhook handler",
									"committedDate": "` + now.Format(time.RFC3339) + `",
									"author": {
										"name": "Alice Developer",
										"user": {"login": "alice"}
									}
								}
							]
						}
					}
				},
				"vulnerabilityAlerts": {
					"totalCount": 1,
					"nodes": [
						{
							"securityVulnerability": {
								"severity": "HIGH",
								"package": {"name": "golang.org/x/crypto"}
							}
						}
					]
				},
				"pullRequests": {
					"nodes": [
						{
							"number": 101,
							"title": "feat: direct query refresh support",
							"author": {"login": "bob"},
							"createdAt": "` + now.Add(-2*time.Hour).Format(time.RFC3339) + `",
							"updatedAt": "` + now.Format(time.RFC3339) + `",
							"isDraft": false,
							"reviewDecision": "APPROVED",
							"headRefName": "feature/refresh",
							"url": "https://github.com/corp-org/payment-api/pull/101",
							"labels": {
								"nodes": [{"name": "enhancement"}]
							},
							"comments": {"totalCount": 2},
							"reviewThreads": {
								"totalCount": 1,
								"nodes": [{"isResolved": true}]
							}
						}
					]
				}
			}
		}
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/graphql" && r.Method == "POST" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(mockGraphQL))
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

	repo, prs, events, secSummary, err := client.FetchSingleRepo(context.Background(), "corp-org", "payment-api")
	if err != nil {
		t.Fatalf("FetchSingleRepo failed: %v", err)
	}

	if repo == nil || repo.Name != "payment-api" {
		t.Fatalf("expected repo payment-api, got %+v", repo)
	}
	if repo.FullName != "corp-org/payment-api" {
		t.Errorf("expected fullName corp-org/payment-api, got %s", repo.FullName)
	}
	if len(prs) != 1 {
		t.Fatalf("expected 1 PR, got %d", len(prs))
	}
	if prs[0].Number != 101 || prs[0].ReviewDecision != "APPROVED" {
		t.Errorf("unexpected PR: %+v", prs[0])
	}
	if prs[0].HeadRefName != "feature/refresh" {
		t.Errorf("expected branch feature/refresh, got %s", prs[0].HeadRefName)
	}
	if len(events) != 1 || events[0].Actor != "alice" {
		t.Errorf("unexpected commit events: %+v", events)
	}
	if secSummary == nil || secSummary.High != 1 || secSummary.Total != 1 {
		t.Errorf("unexpected secSummary: %+v", secSummary)
	}
}

func TestFetchRepositoriesAndPRs_PaginatesOver25CVEs(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	makeAlertNodes := func(crit, high, mod, low int) []map[string]interface{} {
		var nodes []map[string]interface{}
		appendSev := func(sev string, n int) {
			for i := 0; i < n; i++ {
				nodes = append(nodes, map[string]interface{}{
					"securityVulnerability": map[string]interface{}{
						"severity": sev,
						"package":  map[string]string{"name": fmt.Sprintf("pkg-%s-%d", sev, i)},
					},
				})
			}
		}
		appendSev("CRITICAL", crit)
		appendSev("HIGH", high)
		appendSev("MODERATE", mod)
		appendSev("LOW", low)
		return nodes
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" || r.Method != "POST" {
			http.NotFound(w, r)
			return
		}
		var reqBody struct {
			Query     string                 `json:"query"`
			Variables map[string]interface{} `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		if strings.Contains(reqBody.Query, "repositoryOwner") {
			resp := map[string]interface{}{
				"data": map[string]interface{}{
					"repositoryOwner": map[string]interface{}{
						"repositories": map[string]interface{}{
							"pageInfo": map[string]interface{}{"hasNextPage": false, "endCursor": nil},
							"nodes": []map[string]interface{}{
								{
									"databaseId":    1001,
									"name":          "massive-cve-repo",
									"nameWithOwner": "corp-org/massive-cve-repo",
									"owner":         map[string]string{"login": "corp-org"},
									"pushedAt":      now.Format(time.RFC3339),
									"updatedAt":     now.Format(time.RFC3339),
									"url":           "https://github.com/corp-org/massive-cve-repo",
									"vulnerabilityAlerts": map[string]interface{}{
										"totalCount": 495,
										"pageInfo":   map[string]interface{}{"hasNextPage": true, "endCursor": "va-cursor-1"},
										"nodes":      makeAlertNodes(5, 10, 8, 2), // 25 in first page
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

		if strings.Contains(reqBody.Query, "vulnerabilityAlerts(first: 100") {
			cursorVal, _ := reqBody.Variables["cursor"].(string)
			var pageNodes []map[string]interface{}
			hasNext := false
			nextCursor := ""
			switch cursorVal {
			case "va-cursor-1":
				// 100 alerts
				pageNodes = makeAlertNodes(10, 40, 30, 20)
				hasNext = true
				nextCursor = "va-cursor-2"
			case "va-cursor-2":
				// 200 alerts simulated across 2 pages -> 100 here
				pageNodes = makeAlertNodes(15, 35, 35, 15)
				hasNext = true
				nextCursor = "va-cursor-3"
			case "va-cursor-3":
				// 100 alerts
				pageNodes = makeAlertNodes(10, 40, 40, 10)
				hasNext = true
				nextCursor = "va-cursor-4"
			case "va-cursor-4":
				// 100 alerts
				pageNodes = makeAlertNodes(5, 45, 35, 15)
				hasNext = true
				nextCursor = "va-cursor-5"
			case "va-cursor-5":
				// Remaining 70 alerts -> total 25 + 100 + 100 + 100 + 100 + 70 = 495
				pageNodes = makeAlertNodes(5, 25, 25, 15)
				hasNext = false
			}

			var endPtr interface{}
			if nextCursor != "" {
				endPtr = nextCursor
			}
			resp := map[string]interface{}{
				"data": map[string]interface{}{
					"repository": map[string]interface{}{
						"vulnerabilityAlerts": map[string]interface{}{
							"totalCount": 495,
							"pageInfo":   map[string]interface{}{"hasNextPage": hasNext, "endCursor": endPtr},
							"nodes":      pageNodes,
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		if strings.Contains(reqBody.Query, "search(query:") {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"search": map[string]interface{}{
						"issueCount": 0,
						"pageInfo":   map[string]interface{}{"hasNextPage": false, "endCursor": nil},
						"nodes":      []interface{}{},
					},
				},
			})
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": map[string]interface{}{}})
	}))
	defer server.Close()

	client := &Client{
		token:      "dummy-token",
		httpClient: server.Client(),
		apiBaseURL: server.URL,
	}

	_, _, _, secMap, err := client.FetchRepositoriesAndPRs(context.Background(), "corp-org")
	if err != nil {
		t.Fatalf("FetchRepositoriesAndPRs failed: %v", err)
	}

	sum, ok := secMap["massive-cve-repo"]
	if !ok {
		t.Fatalf("expected security summary for massive-cve-repo")
	}

	// Expected totals:
	// Crit: 5 + 10 + 15 + 10 + 5 + 5 = 50
	// High: 10 + 40 + 35 + 40 + 45 + 25 = 195
	// Med:  8 + 30 + 35 + 40 + 35 + 25 = 173
	// Low:  2 + 20 + 15 + 10 + 15 + 15 = 77
	// Sum:  50 + 195 + 173 + 77 = 495
	if sum.Total != 495 {
		t.Errorf("expected Total 495, got %d", sum.Total)
	}
	if sum.Critical != 50 || sum.High != 195 || sum.Medium != 173 || sum.Low != 77 {
		t.Errorf("expected (C=50, H=195, M=173, L=77), got (C=%d, H=%d, M=%d, L=%d)", sum.Critical, sum.High, sum.Medium, sum.Low)
	}
}
