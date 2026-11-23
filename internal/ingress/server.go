package ingress

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"grubby/internal/eventbus"
	"grubby/internal/github"
	"grubby/internal/protocol"
	"grubby/internal/storage"
)

type Config struct {
	Enabled          bool   `yaml:"enabled"`
	Port             int    `yaml:"port"`
	Path             string `yaml:"path"`
	HomeOrg          string `yaml:"home_org"`
	CentralTasksRepo string `yaml:"central_tasks_repo"`
}

type Server struct {
	cfg           Config
	db            *storage.DB
	bus           *eventbus.Bus
	httpSrv       *http.Server
	webhooksCount int64
	mu            sync.Mutex
}

func NewServer(cfg Config, db *storage.DB, bus *eventbus.Bus) *Server {
	if cfg.Port <= 0 {
		cfg.Port = 8484
	}
	if cfg.Path == "" {
		cfg.Path = "/webhooks/github"
	}
	return &Server{
		cfg: cfg,
		db:  db,
		bus: bus,
	}
}

func (s *Server) WebhooksProcessed() int64 {
	return atomic.LoadInt64(&s.webhooksCount)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(s.cfg.Path, s.handleWebhook)
	mux.HandleFunc("/api/v1/events", s.handleSSE)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	return mux
}

func (s *Server) Start(ctx context.Context) error {
	addr := fmt.Sprintf("127.0.0.1:%d", s.cfg.Port)
	s.httpSrv = &http.Server{
		Addr:    addr,
		Handler: s.Handler(),
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.httpSrv.Shutdown(shutdownCtx)
	}()

	if err := s.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("ingress server error: %w", err)
	}
	return nil
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Extract Event Type: check GitHub standard header or Hookdeck forwarded header
	eventType := r.Header.Get("X-GitHub-Event")
	if eventType == "" {
		eventType = r.Header.Get("x-hookdeck-event-type")
	}
	if eventType == "" {
		// Ping event or unknown
		eventType = "ping"
	}

	if eventType == "ping" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"pong"}`))
		return
	}

	// Extract Delivery ID for deduplication
	deliveryID := r.Header.Get("X-GitHub-Delivery")
	if deliveryID == "" {
		deliveryID = r.Header.Get("x-hookdeck-event-id")
	}
	if deliveryID == "" {
		// Generate synthetic delivery ID from hash of payload
		deliveryID = fmt.Sprintf("syn-%d-%d", time.Now().UnixNano(), len(body))
	}

	// Deduplicate against SQLite processed_events table
	if s.db != nil {
		inserted, err := s.db.RecordProcessedEvent(deliveryID, eventType)
		if err == nil && !inserted {
			// Already processed this delivery: respond HTTP 200 without reprocessing
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"duplicate_skipped"}`))
			return
		}
	}

	atomic.AddInt64(&s.webhooksCount, 1)

	// Route event to handler
	var processErr error
	switch eventType {
	case "repository":
		processErr = s.processRepository(body)
	case "pull_request":
		processErr = s.processPullRequest(body)
	case "pull_request_review":
		processErr = s.processPullRequestReview(body)
	case "issues":
		processErr = s.processIssues(body)
	case "discussion":
		processErr = s.processDiscussion(body)
	case "discussion_comment":
		processErr = s.processDiscussionComment(body)
	case "push":
		processErr = s.processPush(body)
	default:
		// Unsupported or informational event (e.g. star, watch, fork)
	}

	if processErr != nil {
		http.Error(w, fmt.Sprintf("Error processing event: %v", processErr), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"processed","event":"` + eventType + `"}`))
}

// --- Event Processors ---

type rawRepoPayload struct {
	Action     string `json:"action"`
	Repository struct {
		ID            int64  `json:"id"`
		Name          string `json:"name"`
		FullName      string `json:"full_name"`
		Description   string `json:"description"`
		DefaultBranch string `json:"default_branch"`
		Private       bool   `json:"private"`
		HTMLURL       string `json:"html_url"`
		SSHURL        string `json:"ssh_url"`
		PushedAt      any    `json:"pushed_at"` // string or unix int
		UpdatedAt     string `json:"updated_at"`
		Owner         struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
}

func parseTimeField(val any) time.Time {
	if val == nil {
		return time.Now()
	}
	switch v := val.(type) {
	case string:
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t
		}
		if t, err := time.Parse("2006-01-02T15:04:05Z", v); err == nil {
			return t
		}
	case float64:
		return time.Unix(int64(v), 0)
	case int64:
		return time.Unix(v, 0)
	case int:
		return time.Unix(int64(v), 0)
	}
	return time.Now()
}

func (s *Server) isHomeOrg(owner string) bool {
	if s.cfg.HomeOrg != "" && strings.EqualFold(s.cfg.HomeOrg, owner) {
		return true
	}
	return false
}

func (s *Server) processRepository(body []byte) error {
	var payload rawRepoPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return err
	}

	repo := github.Repository{
		ID:            payload.Repository.ID,
		Name:          payload.Repository.Name,
		FullName:      payload.Repository.FullName,
		Owner:         payload.Repository.Owner.Login,
		Description:   payload.Repository.Description,
		DefaultBranch: payload.Repository.DefaultBranch,
		IsPrivate:     payload.Repository.Private,
		URL:           payload.Repository.HTMLURL,
		SSHURL:        payload.Repository.SSHURL,
		PushedAt:      parseTimeField(payload.Repository.PushedAt),
		UpdatedAt:     parseTimeField(payload.Repository.UpdatedAt),
		IsHomeOrg:     s.isHomeOrg(payload.Repository.Owner.Login),
	}
	if repo.FullName == "" {
		repo.FullName = repo.Owner + "/" + repo.Name
	}
	if repo.DefaultBranch == "" {
		repo.DefaultBranch = "main"
	}

	action := protocol.ActionUpdated
	if payload.Action == "deleted" {
		action = protocol.ActionDeleted
		if s.db != nil {
			_ = s.db.DeleteRepository(repo.FullName)
		}
	} else {
		if payload.Action == "created" {
			action = protocol.ActionCreated
		}
		if s.db != nil {
			_ = s.db.SaveRepository(repo, repo.IsHomeOrg)
		}
	}

	if s.bus != nil {
		evt, err := protocol.NewRepositoryEvent(action, repo)
		if err == nil {
			s.bus.Publish(evt)
		}
	}
	return nil
}

type rawPRPayload struct {
	Action      string `json:"action"`
	Number      int    `json:"number"`
	PullRequest struct {
		Number             int    `json:"number"`
		Title              string `json:"title"`
		State              string `json:"state"`
		Draft              bool   `json:"draft"`
		HTMLURL            string `json:"html_url"`
		CreatedAt          string `json:"created_at"`
		UpdatedAt          string `json:"updated_at"`
		Comments           int    `json:"comments"`
		ReviewComments     int    `json:"review_comments"`
		RequestedReviewers []struct {
			Login string `json:"login"`
		} `json:"requested_reviewers"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
		Head struct {
			Ref string `json:"ref"`
		} `json:"head"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	} `json:"pull_request"`
	Repository struct {
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		Owner    struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
}

func (s *Server) processPullRequest(body []byte) error {
	var payload rawPRPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return err
	}

	var labels []string
	for _, l := range payload.PullRequest.Labels {
		labels = append(labels, l.Name)
	}

	reviewDecision := "NONE"
	if len(payload.PullRequest.RequestedReviewers) > 0 {
		reviewDecision = "REVIEW_REQUIRED"
	}

	pr := github.PullRequest{
		Number:             payload.PullRequest.Number,
		Repo:               payload.Repository.Name,
		RepoOwner:          payload.Repository.Owner.Login,
		RepoFullName:       payload.Repository.FullName,
		Title:              payload.PullRequest.Title,
		Author:             payload.PullRequest.User.Login,
		State:              strings.ToUpper(payload.PullRequest.State),
		IsDraft:            payload.PullRequest.Draft,
		HeadRefName:        payload.PullRequest.Head.Ref,
		URL:                payload.PullRequest.HTMLURL,
		Labels:             labels,
		TotalComments:      payload.PullRequest.Comments + payload.PullRequest.ReviewComments,
		ReviewDecision:     reviewDecision,
		CreatedAt:          parseTimeField(payload.PullRequest.CreatedAt),
		UpdatedAt:          parseTimeField(payload.PullRequest.UpdatedAt),
	}
	if pr.RepoFullName == "" {
		pr.RepoFullName = pr.RepoOwner + "/" + pr.Repo
	}

	action := protocol.ActionUpdated
	if payload.Action == "opened" {
		action = protocol.ActionCreated
	} else if payload.Action == "closed" {
		action = protocol.ActionDeleted
	}

	if s.db != nil {
		if action == protocol.ActionDeleted {
			_ = s.db.DeletePullRequest(pr.RepoFullName, pr.Number)
		} else {
			_ = s.db.SavePullRequest(pr)
		}

		// Record squad activity
		actor := payload.Sender.Login
		if actor == "" {
			actor = pr.Author
		}
		actString := "Updated PR"
		if payload.Action == "opened" {
			actString = "Opened PR"
		} else if payload.Action == "closed" {
			actString = "Closed PR"
		} else if payload.Action == "synchronize" {
			actString = "Pushed Commits to PR"
		}
		squadEvt := github.SquadEvent{
			Actor:     actor,
			Action:    actString,
			Repo:      pr.RepoFullName,
			Target:    fmt.Sprintf("%s#%d", pr.Repo, pr.Number),
			Details:   pr.Title,
			Timestamp: time.Now(),
			URL:       pr.URL,
		}
		_ = s.db.SaveSquadEvents([]github.SquadEvent{squadEvt})

		if s.bus != nil {
			actEvt, err := protocol.NewSquadActivityEvent(protocol.ActionCreated, squadEvt)
			if err == nil {
				s.bus.Publish(actEvt)
			}
		}
	}

	if s.bus != nil {
		evt, err := protocol.NewPullRequestEvent(action, pr)
		if err == nil {
			s.bus.Publish(evt)
		}
	}
	return nil
}

type rawReviewPayload struct {
	Action string `json:"action"`
	Review struct {
		State  string `json:"state"` // "approved", "changes_requested", "commented"
		HTMLURL string `json:"html_url"`
		User   struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"review"`
	PullRequest struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		User   struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"pull_request"`
	Repository struct {
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		Owner    struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
}

func (s *Server) processPullRequestReview(body []byte) error {
	var payload rawReviewPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return err
	}

	reviewState := strings.ToLower(payload.Review.State)
	reviewDecision := "NONE"
	actString := "Reviewed PR"
	switch reviewState {
	case "approved":
		reviewDecision = "APPROVED"
		actString = "Approved PR"
	case "changes_requested":
		reviewDecision = "CHANGES_REQUESTED"
		actString = "Requested Changes on PR"
	case "commented":
		actString = "Commented on PR"
	}

	// Update pull request in SQLite if present
	if s.db != nil {
		prs, err := s.db.GetPullRequests()
		if err == nil {
			for _, p := range prs {
				if p.Number == payload.PullRequest.Number &&
					(strings.EqualFold(p.Repo, payload.Repository.Name) || strings.EqualFold(p.RepoFullName, payload.Repository.FullName)) {
					p.ReviewDecision = reviewDecision
					p.UpdatedAt = time.Now()
					_ = s.db.SavePullRequest(p)

					if s.bus != nil {
						evt, err := protocol.NewPullRequestEvent(protocol.ActionUpdated, p)
						if err == nil {
							s.bus.Publish(evt)
						}
					}
					break
				}
			}
		}

		// Record squad activity
		squadEvt := github.SquadEvent{
			Actor:     payload.Review.User.Login,
			Action:    actString,
			Repo:      payload.Repository.FullName,
			Target:    fmt.Sprintf("%s#%d", payload.Repository.Name, payload.PullRequest.Number),
			Details:   payload.PullRequest.Title,
			Timestamp: time.Now(),
			URL:       payload.Review.HTMLURL,
		}
		_ = s.db.SaveSquadEvents([]github.SquadEvent{squadEvt})

		if s.bus != nil {
			actEvt, err := protocol.NewSquadActivityEvent(protocol.ActionCreated, squadEvt)
			if err == nil {
				s.bus.Publish(actEvt)
			}
		}
	}

	return nil
}

type rawIssuesPayload struct {
	Action string `json:"action"`
	Issue  struct {
		ID        int64  `json:"id"`
		Number    int    `json:"number"`
		Title     string `json:"title"`
		Body      string `json:"body"`
		State     string `json:"state"` // "open", "closed"
		HTMLURL   string `json:"html_url"`
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
		ClosedAt  *string `json:"closed_at"`
		User      struct {
			Login string `json:"login"`
		} `json:"user"`
		Assignees []struct {
			Login string `json:"login"`
		} `json:"assignees"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	} `json:"issue"`
	Repository struct {
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		Owner    struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
}

func (s *Server) processIssues(body []byte) error {
	var payload rawIssuesPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return err
	}

	var labels []string
	isSquad := false
	for _, l := range payload.Issue.Labels {
		labels = append(labels, l.Name)
		if strings.HasPrefix(strings.ToLower(l.Name), "squad:") {
			isSquad = true
		}
	}

	var assignees []string
	for _, a := range payload.Issue.Assignees {
		assignees = append(assignees, a.Login)
	}

	priority := github.ParsePriority(labels)
	isHome := s.isHomeOrg(payload.Repository.Owner.Login)
	if !isHome && s.cfg.CentralTasksRepo != "" {
		isHome = strings.EqualFold(s.cfg.CentralTasksRepo, payload.Repository.FullName)
	}

	task := github.Task{
		ID:          payload.Issue.ID,
		Number:      payload.Issue.Number,
		Title:       payload.Issue.Title,
		Body:        payload.Issue.Body,
		State:       payload.Issue.State,
		Priority:    priority,
		RepoOwner:   payload.Repository.Owner.Login,
		RepoName:    payload.Repository.Name,
		IsHomeRepo:  isHome,
		IsSquadTask: isSquad,
		Author:      payload.Issue.User.Login,
		Assignees:   assignees,
		Labels:      labels,
		URL:         payload.Issue.HTMLURL,
		CreatedAt:   parseTimeField(payload.Issue.CreatedAt),
		UpdatedAt:   parseTimeField(payload.Issue.UpdatedAt),
	}
	if payload.Issue.ClosedAt != nil {
		cl := parseTimeField(*payload.Issue.ClosedAt)
		task.ClosedAt = &cl
	}

	action := protocol.ActionUpdated
	if payload.Action == "opened" {
		action = protocol.ActionCreated
	} else if payload.Action == "deleted" {
		action = protocol.ActionDeleted
	}

	if s.db != nil {
		if action == protocol.ActionDeleted {
			_ = s.db.DeleteTask(task.ID)
		} else {
			_ = s.db.SaveTask(&task, "synced")
		}

		// Record squad activity if relevant
		actString := "Updated Task"
		if payload.Action == "opened" {
			actString = "Created Task"
		} else if payload.Action == "closed" {
			actString = "Completed Task"
		}
		squadEvt := github.SquadEvent{
			Actor:     payload.Sender.Login,
			Action:    actString,
			Repo:      payload.Repository.FullName,
			Target:    fmt.Sprintf("%s#%d", payload.Repository.Name, task.Number),
			Details:   task.Title,
			Timestamp: time.Now(),
			URL:       task.URL,
		}
		_ = s.db.SaveSquadEvents([]github.SquadEvent{squadEvt})
	}

	if s.bus != nil {
		evt, err := protocol.NewTaskEvent(action, task)
		if err == nil {
			s.bus.Publish(evt)
		}
	}
	return nil
}

type rawPushPayload struct {
	Ref        string `json:"ref"`
	HeadCommit *struct {
		ID        string `json:"id"`
		Message   string `json:"message"`
		Timestamp string `json:"timestamp"`
		URL       string `json:"url"`
		Author    struct {
			Username string `json:"username"`
			Name     string `json:"name"`
		} `json:"author"`
	} `json:"head_commit"`
	Repository struct {
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		PushedAt any    `json:"pushed_at"`
		Owner    struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
}

func (s *Server) processPush(body []byte) error {
	var payload rawPushPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return err
	}

	pushedAt := parseTimeField(payload.Repository.PushedAt)
	if s.db != nil {
		repo, err := s.db.GetRepoByName(payload.Repository.FullName)
		if err == nil && repo != nil {
			repo.PushedAt = pushedAt
			repo.UpdatedAt = pushedAt
			_ = s.db.SaveRepository(*repo, repo.IsHomeOrg)

			if s.bus != nil {
				evt, err := protocol.NewRepositoryEvent(protocol.ActionUpdated, *repo)
				if err == nil {
					s.bus.Publish(evt)
				}
			}
		}

		if payload.HeadCommit != nil {
			actor := payload.Sender.Login
			if actor == "" {
				actor = payload.HeadCommit.Author.Username
			}
			if actor == "" {
				actor = payload.HeadCommit.Author.Name
			}
			branch := strings.TrimPrefix(payload.Ref, "refs/heads/")
			msg := strings.Split(payload.HeadCommit.Message, "\n")[0]
			if len(msg) > 60 {
				msg = msg[:57] + "..."
			}

			squadEvt := github.SquadEvent{
				Actor:     actor,
				Action:    "Committed",
				Repo:      payload.Repository.FullName,
				Target:    fmt.Sprintf("%s:%s", payload.Repository.Name, branch),
				Details:   msg,
				Timestamp: parseTimeField(payload.HeadCommit.Timestamp),
				URL:       payload.HeadCommit.URL,
			}
			_ = s.db.SaveSquadEvents([]github.SquadEvent{squadEvt})

			if s.bus != nil {
				actEvt, err := protocol.NewSquadActivityEvent(protocol.ActionCreated, squadEvt)
				if err == nil {
					s.bus.Publish(actEvt)
				}
			}
		}
	}
	return nil
}

// --- Server-Sent Events (SSE) Stream Endpoint ---

type rawDiscussionPayload struct {
	Action     string `json:"action"`
	Discussion struct {
		ID        int64  `json:"id"`
		NodeID    string `json:"node_id"`
		Number    int    `json:"number"`
		Title     string `json:"title"`
		Body      string `json:"body"`
		HTMLURL   string `json:"html_url"`
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
		User      struct {
			Login string `json:"login"`
		} `json:"user"`
		Category struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
			Slug string `json:"slug"`
		} `json:"category"`
		Comments int    `json:"comments"`
		State    string `json:"state"`
		ClosedAt string `json:"closed_at"`
	} `json:"discussion"`
	Repository struct {
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		Owner    struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
}

type rawDiscussionCommentPayload struct {
	Action  string `json:"action"`
	Comment struct {
		ID        int64  `json:"id"`
		NodeID    string `json:"node_id"`
		Body      string `json:"body"`
		HTMLURL   string `json:"html_url"`
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
		User      struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"comment"`
	Discussion struct {
		ID        int64  `json:"id"`
		NodeID    string `json:"node_id"`
		Number    int    `json:"number"`
		Title     string `json:"title"`
		Body      string `json:"body"`
		HTMLURL   string `json:"html_url"`
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
		User      struct {
			Login string `json:"login"`
		} `json:"user"`
		Category struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
			Slug string `json:"slug"`
		} `json:"category"`
		Comments int    `json:"comments"`
		State    string `json:"state"`
		ClosedAt string `json:"closed_at"`
	} `json:"discussion"`
	Repository struct {
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		Owner    struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
}

func (s *Server) processDiscussion(body []byte) error {
	var payload rawDiscussionPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return err
	}

	discID := payload.Discussion.NodeID
	if discID == "" {
		discID = fmt.Sprintf("D_%d", payload.Discussion.ID)
	}

	disc := github.Discussion{
		ID:            discID,
		Number:        payload.Discussion.Number,
		RepoOwner:     payload.Repository.Owner.Login,
		RepoName:      payload.Repository.Name,
		Title:         payload.Discussion.Title,
		Body:          payload.Discussion.Body,
		Author:        payload.Discussion.User.Login,
		CreatedAt:     parseTimeField(payload.Discussion.CreatedAt),
		UpdatedAt:     parseTimeField(payload.Discussion.UpdatedAt),
		URL:           payload.Discussion.HTMLURL,
		CommentsCount: payload.Discussion.Comments,
		Category: github.DiscussionCategory{
			ID:   fmt.Sprintf("%d", payload.Discussion.Category.ID),
			Name: payload.Discussion.Category.Name,
			Slug: payload.Discussion.Category.Slug,
		},
	}

	isClosed := (payload.Action == "closed" || payload.Discussion.State == "closed" || payload.Discussion.ClosedAt != "")
	if payload.Action == "reopened" {
		isClosed = false
	}
	disc.Closed = isClosed
	if isClosed {
		disc.State = "closed"
	} else {
		disc.State = "open"
	}

	action := protocol.ActionUpdated
	switch payload.Action {
	case "created":
		action = protocol.ActionCreated
	case "deleted":
		action = protocol.ActionDeleted
	}

	if s.db != nil {
		_ = s.db.SaveDiscussion(disc, disc.Category.Name)
	}

	if s.bus != nil {
		evt, err := protocol.NewDiscussionEvent(action, disc, nil)
		if err == nil {
			s.bus.Publish(evt)
		}
	}
	return nil
}

func (s *Server) processDiscussionComment(body []byte) error {
	var payload rawDiscussionCommentPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return err
	}

	discID := payload.Discussion.NodeID
	if discID == "" {
		discID = fmt.Sprintf("D_%d", payload.Discussion.ID)
	}

	cmID := payload.Comment.NodeID
	if cmID == "" {
		cmID = fmt.Sprintf("DC_%d", payload.Comment.ID)
	}

	disc := github.Discussion{
		ID:            discID,
		Number:        payload.Discussion.Number,
		RepoOwner:     payload.Repository.Owner.Login,
		RepoName:      payload.Repository.Name,
		Title:         payload.Discussion.Title,
		Body:          payload.Discussion.Body,
		Author:        payload.Discussion.User.Login,
		CreatedAt:     parseTimeField(payload.Discussion.CreatedAt),
		UpdatedAt:     parseTimeField(payload.Discussion.UpdatedAt),
		URL:           payload.Discussion.HTMLURL,
		CommentsCount: payload.Discussion.Comments,
		Category: github.DiscussionCategory{
			ID:   fmt.Sprintf("%d", payload.Discussion.Category.ID),
			Name: payload.Discussion.Category.Name,
			Slug: payload.Discussion.Category.Slug,
		},
	}

	isClosed := (payload.Action == "closed" || payload.Discussion.State == "closed" || payload.Discussion.ClosedAt != "")
	if payload.Action == "reopened" {
		isClosed = false
	}
	disc.Closed = isClosed
	if isClosed {
		disc.State = "closed"
	} else {
		disc.State = "open"
	}

	cm := github.DiscussionComment{
		ID:           cmID,
		DiscussionID: discID,
		Author:       payload.Comment.User.Login,
		Body:         payload.Comment.Body,
		CreatedAt:    parseTimeField(payload.Comment.CreatedAt),
		UpdatedAt:    parseTimeField(payload.Comment.UpdatedAt),
		URL:          payload.Comment.HTMLURL,
	}

	if s.db != nil {
		_ = s.db.SaveDiscussion(disc, disc.Category.Name)
		_ = s.db.SaveDiscussionComment(cm)
	}

	if s.bus != nil {
		evt, err := protocol.NewDiscussionEvent(protocol.ActionUpdated, disc, &cm)
		if err == nil {
			s.bus.Publish(evt)
		}
	}
	return nil
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	orgFilter := r.URL.Query().Get("org")
	entityFilter := r.URL.Query().Get("entity")

	sub := s.bus.Subscribe(func(e protocol.EventEnvelope) bool {
		if orgFilter != "" && !strings.EqualFold(e.Org, orgFilter) {
			return false
		}
		if entityFilter != "" && string(e.Entity) != entityFilter {
			return false
		}
		return true
	})
	defer sub.Unsubscribe()

	// Initial ping
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			return
		case evt, ok := <-sub.C:
			if !ok {
				return
			}
			data, err := json.Marshal(evt)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s.%s\nid: %s\ndata: %s\n\n", evt.Entity, evt.Action, evt.ID, string(data))
			flusher.Flush()
		}
	}
}
