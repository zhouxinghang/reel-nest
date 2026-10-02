package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"reelnest/internal/model"
)

const localPerson = ` FROM title_credits c JOIN people p ON p.id=c.person_id AND p.catalog_id=c.catalog_id JOIN provider_catalogs pc ON pc.id=p.catalog_id
 JOIN metadata_snapshots sn ON sn.remote_title_id=c.remote_title_id AND sn.expires_at>?
 JOIN metadata_bindings b ON b.remote_title_id=c.remote_title_id JOIN metadata_subjects sub ON sub.id=b.subject_id
 JOIN catalog_entries e ON e.media_file_id=sub.media_file_id OR e.series_id=sub.series_id WHERE p.id=?`

const personDetailSQL = `SELECT p.id,c.name,pc.source,p.catalog_id,(SELECT a.id FROM assets a WHERE a.credit_id=c.id)` + localPerson + ` ORDER BY e.available_file_count>0 DESC,c.remote_title_id,c.role,c.ordinal,e.entry_key LIMIT 1`
const personWorksSQL = `WITH candidates AS (SELECT DISTINCT c.remote_title_id,e.entry_key,e.available_file_count` + localPerson + `), ranked AS (SELECT *,row_number() OVER(PARTITION BY remote_title_id ORDER BY available_file_count>0 DESC,entry_key) AS n FROM candidates)
 SELECT remote_title_id,entry_key FROM ranked WHERE n=1 AND remote_title_id>? ORDER BY remote_title_id LIMIT ?`

func (s *Store) MetadataPerson(ctx context.Context, id string, now int64) (model.Person, error) {
	var p model.Person
	e := s.reader.QueryRowContext(ctx, personDetailSQL, now, id).Scan(&p.ID, &p.Name, &p.Source, &p.CatalogID, &p.PhotoAssetID)
	return p, notFound(e)
}

// PersonWorks pages distinct remote works, choosing an available local copy
// first. Credits and card summaries are batched only for the selected page.
func (s *Store) MetadataPersonWorks(ctx context.Context, id, after string, limit int, expected, now int64) ([]model.PersonWork, int64, int64, error) {
	works := []model.PersonWork{}
	if limit < 1 || limit > 101 {
		return works, 0, 0, model.ErrInvalid
	}
	tx, err := s.reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return works, 0, 0, err
	}
	defer tx.Rollback()
	var revision int64
	if err = tx.QueryRowContext(ctx, `SELECT catalog FROM revisions WHERE singleton=1`).Scan(&revision); err != nil {
		return works, 0, 0, err
	}
	if expected != 0 && expected != revision {
		return works, revision, 0, model.ErrCursor
	}
	var expiry *int64
	if err = tx.QueryRowContext(ctx, `SELECT min(sn.expires_at)`+localPerson, now, id).Scan(&expiry); err != nil {
		return works, 0, 0, err
	}
	if expiry == nil {
		return works, revision, 0, model.ErrNotFound
	}
	rows, err := tx.QueryContext(ctx, personWorksSQL, now, id, after, limit)
	if err != nil {
		return works, 0, 0, err
	}
	keys, remotes := []string{}, []string{}
	for rows.Next() {
		var key, remote string
		if err = rows.Scan(&remote, &key); err != nil {
			break
		}
		keys = append(keys, key)
		remotes = append(remotes, remote)
	}
	err = errors.Join(err, rows.Err())
	rows.Close()
	if err != nil {
		return works, 0, 0, err
	}
	cards, err := readCards(ctx, tx, keys, now)
	if err != nil {
		return works, 0, 0, err
	}
	for _, c := range cards {
		works = append(works, model.PersonWork{CatalogCard: c, Roles: []string{}, Characters: []string{}})
	}
	body, _ := json.Marshal(remotes)
	rows, err = tx.QueryContext(ctx, `SELECT remote_title_id,role,characters FROM title_credits WHERE catalog_id=(SELECT catalog_id FROM people WHERE id=?) AND person_id=? AND remote_title_id IN(SELECT value FROM json_each(?)) ORDER BY remote_title_id,role,ordinal`, id, id, string(body))
	if err != nil {
		return works, 0, 0, err
	}
	index := map[string]int{}
	for i, w := range works {
		index[w.RemoteTitleID] = i
	}
	for rows.Next() {
		var remote, role, body string
		if err = rows.Scan(&remote, &role, &body); err != nil {
			break
		}
		var chars []string
		if err = json.Unmarshal([]byte(body), &chars); err != nil {
			break
		}
		w := &works[index[remote]]
		w.Roles = appendUnique(w.Roles, role, 2)
		for _, v := range chars {
			w.Characters = appendUnique(w.Characters, v, 50)
		}
	}
	err = errors.Join(err, rows.Err())
	rows.Close()
	if err != nil {
		return works, 0, 0, err
	}
	return works, revision, *expiry, tx.Commit()
}
func appendUnique(list []string, v string, limit int) []string {
	for _, s := range list {
		if s == v {
			return list
		}
	}
	if len(list) < limit {
		return append(list, v)
	}
	return list
}
