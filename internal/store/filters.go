package store

import (
	"database/sql"
	"time"
)

const filterCols = `id,source_id,mode,field,keyword,match_type,enabled,created_at`

func scanRule(sc rowScanner) (*FilterRule, error) {
	var r FilterRule
	var sourceID sql.NullInt64
	var enabled int
	var createdAt string
	if err := sc.Scan(&r.ID, &sourceID, &r.Mode, &r.Field, &r.Keyword,
		&r.MatchType, &enabled, &createdAt); err != nil {
		return nil, err
	}
	r.SourceID = scanInt(sourceID)
	r.Enabled = enabled != 0
	r.CreatedAt = mustParseTime(createdAt)
	return &r, nil
}

func (st *Store) CreateRule(r *FilterRule) error {
	r.CreatedAt = time.Now().UTC()
	res, err := st.db.Exec(
		`INSERT INTO filter_rules(source_id,mode,field,keyword,match_type,enabled,created_at)
		 VALUES(?,?,?,?,?,?,?)`,
		nullInt(r.SourceID), r.Mode, r.Field, r.Keyword, r.MatchType, b2i(r.Enabled), fmtTime(r.CreatedAt))
	if err != nil {
		return err
	}
	r.ID, _ = res.LastInsertId()
	return nil
}

// ListRules returns every rule (global and per-source), newest first.
func (st *Store) ListRules() ([]*FilterRule, error) {
	rows, err := st.db.Query(`SELECT ` + filterCols + ` FROM filter_rules ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRules(rows)
}

// RulesForSource returns enabled rules that apply to a source: its own rules plus all globals.
func (st *Store) RulesForSource(sourceID int64) ([]*FilterRule, error) {
	rows, err := st.db.Query(
		`SELECT `+filterCols+` FROM filter_rules WHERE enabled = 1 AND (source_id IS NULL OR source_id = ?)`,
		sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRules(rows)
}

// GlobalRules returns enabled rules with no source (used for ad-hoc URL imports).
func (st *Store) GlobalRules() ([]*FilterRule, error) {
	rows, err := st.db.Query(`SELECT ` + filterCols + ` FROM filter_rules WHERE enabled = 1 AND source_id IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRules(rows)
}

func collectRules(rows *sql.Rows) ([]*FilterRule, error) {
	var out []*FilterRule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (st *Store) UpdateRule(r *FilterRule) error {
	_, err := st.db.Exec(
		`UPDATE filter_rules SET source_id=?,mode=?,field=?,keyword=?,match_type=?,enabled=? WHERE id=?`,
		nullInt(r.SourceID), r.Mode, r.Field, r.Keyword, r.MatchType, b2i(r.Enabled), r.ID)
	return err
}

func (st *Store) DeleteRule(id int64) error {
	_, err := st.db.Exec(`DELETE FROM filter_rules WHERE id = ?`, id)
	return err
}
