package store

import (
	"database/sql"
	"time"
)

const itemCols = `id,source_id,guid,url,title,status,filter_reason,tandoor_recipe_id,raw_tags,error,discovered_at,processed_at`

func scanItem(sc rowScanner) (*Item, error) {
	var it Item
	var sourceID, tandoorID sql.NullInt64
	var discoveredAt string
	var processedAt sql.NullString
	if err := sc.Scan(&it.ID, &sourceID, &it.GUID, &it.URL, &it.Title, &it.Status,
		&it.FilterReason, &tandoorID, &it.RawTags, &it.Error, &discoveredAt, &processedAt); err != nil {
		return nil, err
	}
	it.SourceID = scanInt(sourceID)
	it.TandoorRecipeID = scanInt(tandoorID)
	it.DiscoveredAt = mustParseTime(discoveredAt)
	it.ProcessedAt = scanTime(processedAt)
	return &it, nil
}

// ItemExists reports whether this recipe was already seen, by URL or feed GUID
// (both are effectively global identifiers), so re-polls never re-import.
func (st *Store) ItemExists(guid, url string) (bool, error) {
	var n int
	err := st.db.QueryRow(
		`SELECT COUNT(1) FROM items WHERE url = ? OR (guid != '' AND guid = ?)`,
		url, guid).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (st *Store) CreateItem(it *Item) error {
	if it.DiscoveredAt.IsZero() {
		it.DiscoveredAt = time.Now().UTC()
	}
	res, err := st.db.Exec(
		`INSERT INTO items(source_id,guid,url,title,status,filter_reason,tandoor_recipe_id,raw_tags,error,discovered_at,processed_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		nullInt(it.SourceID), it.GUID, it.URL, it.Title, it.Status, it.FilterReason,
		nullInt(it.TandoorRecipeID), it.RawTags, it.Error, fmtTime(it.DiscoveredAt), nullTime(it.ProcessedAt))
	if err != nil {
		return err
	}
	it.ID, _ = res.LastInsertId()
	return nil
}

// UpdateItem persists the outcome fields after processing.
func (st *Store) UpdateItem(it *Item) error {
	_, err := st.db.Exec(
		`UPDATE items SET status=?,filter_reason=?,tandoor_recipe_id=?,raw_tags=?,error=?,title=?,processed_at=? WHERE id=?`,
		it.Status, it.FilterReason, nullInt(it.TandoorRecipeID), it.RawTags, it.Error,
		it.Title, nullTime(it.ProcessedAt), it.ID)
	return err
}

// ListItems returns the most recent items, optionally filtered by status (empty = all).
func (st *Store) ListItems(status string, limit int) ([]*Item, error) {
	if limit <= 0 {
		limit = 100
	}
	var (
		rows *sql.Rows
		err  error
	)
	if status == "" {
		rows, err = st.db.Query(`SELECT `+itemCols+` FROM items ORDER BY id DESC LIMIT ?`, limit)
	} else {
		rows, err = st.db.Query(`SELECT `+itemCols+` FROM items WHERE status = ? ORDER BY id DESC LIMIT ?`, status, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// StatusCounts returns a map of status -> count for the dashboard.
func (st *Store) StatusCounts() (map[string]int, error) {
	rows, err := st.db.Query(`SELECT status, COUNT(1) FROM items GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			return nil, err
		}
		out[s] = n
	}
	return out, rows.Err()
}
