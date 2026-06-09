package store

import (
	"database/sql"
	"time"
)

const sourceCols = `id,name,feed_url,site_url,enabled,poll_interval_minutes,backlog_on_add,backlog_limit,default_keywords,created_at,last_checked_at,last_status,last_error,seeded`

func scanSource(sc rowScanner) (*Source, error) {
	var s Source
	var enabled, backlog, seeded int
	var createdAt string
	var lastChecked sql.NullString
	if err := sc.Scan(&s.ID, &s.Name, &s.FeedURL, &s.SiteURL, &enabled,
		&s.PollIntervalMinutes, &backlog, &s.BacklogLimit, &s.DefaultKeywords,
		&createdAt, &lastChecked, &s.LastStatus, &s.LastError, &seeded); err != nil {
		return nil, err
	}
	s.Enabled = enabled != 0
	s.BacklogOnAdd = backlog != 0
	s.Seeded = seeded != 0
	s.CreatedAt = mustParseTime(createdAt)
	s.LastCheckedAt = scanTime(lastChecked)
	return &s, nil
}

// MarkSeeded flags a source as having completed its initial feed reconciliation.
func (st *Store) MarkSeeded(id int64) error {
	_, err := st.db.Exec(`UPDATE sources SET seeded = 1 WHERE id = ?`, id)
	return err
}

func (st *Store) CreateSource(s *Source) error {
	s.CreatedAt = time.Now().UTC()
	res, err := st.db.Exec(
		`INSERT INTO sources(name,feed_url,site_url,enabled,poll_interval_minutes,backlog_on_add,backlog_limit,default_keywords,created_at,last_status,last_error)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		s.Name, s.FeedURL, s.SiteURL, b2i(s.Enabled), s.PollIntervalMinutes,
		b2i(s.BacklogOnAdd), s.BacklogLimit, s.DefaultKeywords, fmtTime(s.CreatedAt),
		s.LastStatus, s.LastError)
	if err != nil {
		return err
	}
	s.ID, _ = res.LastInsertId()
	return nil
}

func (st *Store) GetSource(id int64) (*Source, error) {
	return scanSource(st.db.QueryRow(`SELECT `+sourceCols+` FROM sources WHERE id = ?`, id))
}

func (st *Store) ListSources() ([]*Source, error) {
	rows, err := st.db.Query(`SELECT ` + sourceCols + ` FROM sources ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Source
	for rows.Next() {
		s, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListEnabledSources returns sources eligible for scheduled polling.
func (st *Store) ListEnabledSources() ([]*Source, error) {
	rows, err := st.db.Query(`SELECT ` + sourceCols + ` FROM sources WHERE enabled = 1 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Source
	for rows.Next() {
		s, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (st *Store) UpdateSource(s *Source) error {
	_, err := st.db.Exec(
		`UPDATE sources SET name=?,feed_url=?,site_url=?,enabled=?,poll_interval_minutes=?,backlog_on_add=?,backlog_limit=?,default_keywords=? WHERE id=?`,
		s.Name, s.FeedURL, s.SiteURL, b2i(s.Enabled), s.PollIntervalMinutes,
		b2i(s.BacklogOnAdd), s.BacklogLimit, s.DefaultKeywords, s.ID)
	return err
}

// UpdateSourceStatus records the outcome of a poll.
func (st *Store) UpdateSourceStatus(id int64, status, errMsg string) error {
	now := time.Now().UTC()
	_, err := st.db.Exec(
		`UPDATE sources SET last_checked_at=?, last_status=?, last_error=? WHERE id=?`,
		nullTime(&now), status, errMsg, id)
	return err
}

func (st *Store) DeleteSource(id int64) error {
	_, err := st.db.Exec(`DELETE FROM sources WHERE id = ?`, id)
	return err
}
