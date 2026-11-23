package storage

import (
	"database/sql"
	"strings"
	"time"

	"grubby/internal/github"
)

// SaveDiscussions persists a list of discussions and their embedded comments.
func (d *DB) SaveDiscussions(discussions []github.Discussion, squad string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmtDisc, err := tx.Prepare(`
		INSERT INTO discussions (
			id, number, repo_owner, repo_name, category_id, category_name, category_slug,
			squad, title, body, author, created_at, updated_at, url, comments_count, closed
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			number=excluded.number,
			repo_owner=excluded.repo_owner,
			repo_name=excluded.repo_name,
			category_id=excluded.category_id,
			category_name=excluded.category_name,
			category_slug=excluded.category_slug,
			squad=CASE WHEN excluded.squad != '' THEN excluded.squad ELSE discussions.squad END,
			title=excluded.title,
			body=excluded.body,
			author=excluded.author,
			updated_at=excluded.updated_at,
			url=excluded.url,
			comments_count=excluded.comments_count,
			closed=excluded.closed
	`)
	if err != nil {
		return err
	}
	defer stmtDisc.Close()

	stmtCm, err := tx.Prepare(`
		INSERT INTO discussion_comments (
			id, discussion_id, author, body, created_at, updated_at, url
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			author=excluded.author,
			body=excluded.body,
			updated_at=excluded.updated_at,
			url=excluded.url
	`)
	if err != nil {
		return err
	}
	defer stmtCm.Close()

	for _, disc := range discussions {
		discSquad := squad
		if discSquad == "" {
			// Infer squad from category or labels
			for _, l := range disc.Labels {
				if strings.HasPrefix(strings.ToLower(l), "squad:") {
					discSquad = strings.TrimPrefix(strings.ToLower(l), "squad:")
					break
				}
			}
		}

		closedVal := 0
		if disc.IsClosed() {
			closedVal = 1
		}

		_, err := stmtDisc.Exec(
			disc.ID, disc.Number, disc.RepoOwner, disc.RepoName,
			disc.Category.ID, disc.Category.Name, disc.Category.Slug,
			discSquad, disc.Title, disc.Body, disc.Author,
			disc.CreatedAt, disc.UpdatedAt, disc.URL, disc.CommentsCount,
			closedVal,
		)
		if err != nil {
			return err
		}

		for _, cm := range disc.Comments {
			_, err := stmtCm.Exec(
				cm.ID, disc.ID, cm.Author, cm.Body,
				cm.CreatedAt, cm.UpdatedAt, cm.URL,
			)
			if err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

// SaveDiscussion persists a single discussion.
func (d *DB) SaveDiscussion(disc github.Discussion, squad string) error {
	return d.SaveDiscussions([]github.Discussion{disc}, squad)
}

// SaveDiscussionComment persists a single discussion comment and updates discussion comment count.
func (d *DB) SaveDiscussionComment(cm github.DiscussionComment) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.Exec(`
		INSERT INTO discussion_comments (
			id, discussion_id, author, body, created_at, updated_at, url
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			author=excluded.author,
			body=excluded.body,
			updated_at=excluded.updated_at,
			url=excluded.url
	`, cm.ID, cm.DiscussionID, cm.Author, cm.Body, cm.CreatedAt, cm.UpdatedAt, cm.URL)
	if err != nil {
		return err
	}

	// Update discussion updated_at and comments_count
	_, err = tx.Exec(`
		UPDATE discussions 
		SET updated_at = ?, comments_count = (
			SELECT COUNT(*) FROM discussion_comments WHERE discussion_id = ?
		)
		WHERE id = ?
	`, cm.CreatedAt, cm.DiscussionID, cm.DiscussionID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// GetDiscussions retrieves discussions, optionally filtered by squad.
func (d *DB) GetDiscussions(squad string) ([]github.Discussion, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
		SELECT id, number, repo_owner, repo_name, category_id, category_name, category_slug,
		       squad, title, body, author, created_at, updated_at, url, comments_count, last_read_at, closed
		FROM discussions
	`
	var args []interface{}
	if strings.TrimSpace(squad) != "" {
		query += ` WHERE LOWER(squad) = LOWER(?) OR squad = ''`
		args = append(args, squad)
	}
	query += ` ORDER BY updated_at DESC`

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []github.Discussion
	for rows.Next() {
		var disc github.Discussion
		var lastRead sql.NullTime
		var squadVal string
		var closedVal int
		err := rows.Scan(
			&disc.ID, &disc.Number, &disc.RepoOwner, &disc.RepoName,
			&disc.Category.ID, &disc.Category.Name, &disc.Category.Slug,
			&squadVal, &disc.Title, &disc.Body, &disc.Author,
			&disc.CreatedAt, &disc.UpdatedAt, &disc.URL, &disc.CommentsCount,
			&lastRead, &closedVal,
		)
		if err != nil {
			return nil, err
		}
		if lastRead.Valid {
			disc.LastReadAt = lastRead.Time
		}
		disc.Closed = (closedVal != 0)
		if disc.Closed {
			disc.State = "closed"
		} else {
			disc.State = "open"
		}
		results = append(results, disc)
	}
	return results, nil
}

// GetDiscussion retrieves a single discussion by ID.
func (d *DB) GetDiscussion(id string) (*github.Discussion, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
		SELECT id, number, repo_owner, repo_name, category_id, category_name, category_slug,
		       squad, title, body, author, created_at, updated_at, url, comments_count, last_read_at, closed
		FROM discussions
		WHERE id = ?
	`
	var disc github.Discussion
	var lastRead sql.NullTime
	var squadVal string
	var closedVal int
	err := d.db.QueryRow(query, id).Scan(
		&disc.ID, &disc.Number, &disc.RepoOwner, &disc.RepoName,
		&disc.Category.ID, &disc.Category.Name, &disc.Category.Slug,
		&squadVal, &disc.Title, &disc.Body, &disc.Author,
		&disc.CreatedAt, &disc.UpdatedAt, &disc.URL, &disc.CommentsCount,
		&lastRead, &closedVal,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if lastRead.Valid {
		disc.LastReadAt = lastRead.Time
	}
	disc.Closed = (closedVal != 0)
	if disc.Closed {
		disc.State = "closed"
	} else {
		disc.State = "open"
	}
	return &disc, nil
}

// GetDiscussionComments retrieves all comments for a discussion ordered by created_at asc.
func (d *DB) GetDiscussionComments(discussionID string) ([]github.DiscussionComment, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
		SELECT id, discussion_id, author, body, created_at, updated_at, url
		FROM discussion_comments
		WHERE discussion_id = ?
		ORDER BY created_at ASC
	`
	rows, err := d.db.Query(query, discussionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []github.DiscussionComment
	for rows.Next() {
		var cm github.DiscussionComment
		err := rows.Scan(
			&cm.ID, &cm.DiscussionID, &cm.Author, &cm.Body,
			&cm.CreatedAt, &cm.UpdatedAt, &cm.URL,
		)
		if err != nil {
			return nil, err
		}
		comments = append(comments, cm)
	}
	return comments, nil
}

// MarkDiscussionRead updates the last_read_at timestamp for a discussion.
func (d *DB) MarkDiscussionRead(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`UPDATE discussions SET last_read_at = ? WHERE id = ?`, time.Now(), id)
	return err
}

// UpdateDiscussionBody updates the body of a discussion.
func (d *DB) UpdateDiscussionBody(id, body string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`UPDATE discussions SET body = ?, updated_at = ? WHERE id = ?`, body, time.Now(), id)
	return err
}

// UpdateDiscussionCommentBody updates the body of a discussion comment.
func (d *DB) UpdateDiscussionCommentBody(id, body string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`UPDATE discussion_comments SET body = ?, updated_at = ? WHERE id = ?`, body, time.Now(), id)
	return err
}

// MarkMessagesSeen marks a list of message IDs (OP or comments) as seen by the user.
func (d *DB) MarkMessagesSeen(messageIDs []string, discussionID string) error {
	if len(messageIDs) == 0 {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`
		INSERT INTO seen_discussion_messages (message_id, discussion_id, seen_at)
		VALUES (?, ?, ?)
		ON CONFLICT(message_id) DO NOTHING
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now()
	for _, id := range messageIDs {
		if strings.TrimSpace(id) == "" {
			continue
		}
		if _, err := stmt.Exec(id, discussionID, now); err != nil {
			return err
		}
	}

	if discussionID != "" {
		_, _ = tx.Exec(`UPDATE discussions SET last_read_at = ? WHERE id = ?`, now, discussionID)
	}

	return tx.Commit()
}

// GetSeenMessageIDs retrieves a set of all message IDs marked as seen.
func (d *DB) GetSeenMessageIDs() (map[string]bool, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`SELECT message_id FROM seen_discussion_messages`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seen := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			seen[id] = true
		}
	}
	return seen, nil
}

// GetUnreadDiscussionsCount counts discussions that have new activity since last_read_at.
func (d *DB) GetUnreadDiscussionsCount(squad string) (int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
		SELECT COUNT(*)
		FROM discussions
		WHERE (last_read_at IS NULL OR updated_at > last_read_at)
	`
	var args []interface{}
	if strings.TrimSpace(squad) != "" {
		query += ` AND (LOWER(squad) = LOWER(?) OR squad = '')`
		args = append(args, squad)
	}

	var count int
	err := d.db.QueryRow(query, args...).Scan(&count)
	return count, err
}
