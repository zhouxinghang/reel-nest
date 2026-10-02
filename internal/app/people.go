package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Provider IDs identify people across titles. Missing IDs deliberately remain
// title-scoped: sharing a name is not evidence that two people are the same.
func decoratePeople(m *TitleMetadata) {
	if m.Credits == nil {
		return
	}
	decorate := func(person *CreditPerson) {
		person.Key = strconv.FormatInt(person.ID, 10)
		if person.ID <= 0 {
			sum := sha256.Sum256([]byte(m.Kind + ":" + strconv.FormatInt(m.SourceID, 10) + ":" + creditIdentity(*person)))
			person.Key = "local-" + hex.EncodeToString(sum[:16])
		}
		person.PhotoURL = ""
		if person.PhotoPath != "" {
			person.PhotoURL = "/api/people/" + strings.ToLower(m.Source) + "/" + person.Key + "/photo"
		}
	}
	for i := range m.Credits.Cast {
		decorate(&m.Credits.Cast[i].CreditPerson)
	}
	for i := range m.Credits.Directors {
		decorate(&m.Credits.Directors[i])
	}
}

type PersonWork struct {
	ID         string        `json:"id"`
	TargetKind string        `json:"targetKind"` // media or series, for local navigation
	Available  bool          `json:"available"`
	Metadata   TitleMetadata `json:"metadata"`
	Jobs       []string      `json:"jobs"`
	Characters []string      `json:"characters"`
}

type PersonDetail struct {
	CreditPerson
	Source string       `json:"source"`
	Works  []PersonWork `json:"works"`
}

var personKeyPattern = regexp.MustCompile(`^(?:[1-9][0-9]{0,18}|local-[0-9a-f]{32})$`)

func personRequest(r *http.Request) (string, string, bool) {
	source, key := r.PathValue("source"), r.PathValue("key")
	return source, key, validMetadataSource(source) && personKeyPattern.MatchString(key)
}

// Resolve only currently associated, unexpired title metadata. Orphaned caches,
// prior manual selections and removed files cannot create phantom filmographies.
// Organized episodes resolve to one local series; multiple versions of the same
// provider title resolve to one work, preferring an available local target.
func (a *App) personDetails(ctx context.Context, source, key string) (PersonDetail, error) {
	detail := PersonDetail{Source: strings.ToUpper(source), Works: []PersonWork{}}
	rows, err := a.db.QueryContext(ctx, `WITH targets AS (
 SELECT d.kind, f.id, 'media' target_kind, f.available
 FROM media_files f JOIN media_directories d ON d.id=f.directory_id
 LEFT JOIN file_organization o ON o.media_file_id=f.id WHERE o.series_id IS NULL
 UNION ALL
 SELECT 'series',s.id,'series',MAX(f.available) FROM series s
 JOIN file_organization o ON o.series_id=s.id JOIN media_files f ON f.id=o.media_file_id GROUP BY s.id
 ) SELECT t.id,t.target_kind,t.available,p.kind,p.source_id,c.payload,
 p.person_id,p.name,p.photo_path,p.actor,p.director,p.characters FROM metadata_people p
 JOIN metadata_cache c ON c.source=p.source AND c.kind=p.kind AND c.source_id=p.source_id
 JOIN metadata_matches m ON m.source=p.source AND m.kind=p.kind AND m.source_id=p.source_id
 JOIN targets t ON t.kind=m.kind AND t.id=m.target_id
 WHERE p.source=? AND p.person_key=? AND c.expires_at>?
	 ORDER BY m.kind,m.source_id,t.available DESC,t.target_kind DESC,t.id`, source, key, time.Now().Unix())
	if err != nil {
		return detail, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		work := PersonWork{Jobs: []string{}, Characters: []string{}}
		var kind, payload, name, photoPath, characters string
		var sourceID, personID int64
		var actor, director bool
		if err := rows.Scan(&work.ID, &work.TargetKind, &work.Available, &kind, &sourceID, &payload, &personID, &name, &photoPath, &actor, &director, &characters); err != nil {
			return detail, err
		}
		m := &work.Metadata
		if json.Unmarshal([]byte(payload), m) != nil {
			continue
		}
		m.Kind = kind
		m.SourceID = sourceID
		m.Credits = nil // Work cards don't need another full cast in the response.
		person := CreditPerson{ID: personID, Name: name, Key: key, PhotoPath: photoPath}
		if photoPath != "" {
			person.PhotoURL = "/api/people/" + source + "/" + key + "/photo"
		}
		if detail.Key == "" || (detail.PhotoPath == "" && person.PhotoPath != "") {
			detail.CreditPerson = person
		}
		if actor {
			work.Jobs = append(work.Jobs, "演员")
			if json.Unmarshal([]byte(characters), &work.Characters) != nil {
				work.Characters = []string{}
			}
		}
		if director {
			work.Jobs = append(work.Jobs, "导演")
		}
		identity := kind + ":" + strconv.FormatInt(sourceID, 10)
		if len(work.Jobs) > 0 && !seen[identity] {
			seen[identity] = true
			detail.Works = append(detail.Works, work)
		}
	}
	if err := rows.Err(); err != nil {
		return detail, err
	}
	if detail.Key == "" {
		return detail, sql.ErrNoRows
	}
	sort.SliceStable(detail.Works, func(i, j int) bool {
		left, right := detail.Works[i].Metadata, detail.Works[j].Metadata
		if left.Year != right.Year {
			return left.Year > right.Year
		}
		return left.Title < right.Title
	})
	return detail, nil
}

func (a *App) personIdentity(ctx context.Context, source, key string) (CreditPerson, error) {
	var person CreditPerson
	err := a.db.QueryRowContext(ctx, `WITH targets AS (
 SELECT d.kind, f.id FROM media_files f JOIN media_directories d ON d.id=f.directory_id
 LEFT JOIN file_organization o ON o.media_file_id=f.id WHERE o.series_id IS NULL
 UNION
 SELECT 'series',s.id FROM series s JOIN file_organization o ON o.series_id=s.id
 ) SELECT p.person_id,p.name,p.photo_path FROM metadata_people p
 JOIN metadata_cache c ON c.source=p.source AND c.kind=p.kind AND c.source_id=p.source_id
 JOIN metadata_matches m ON m.source=p.source AND m.kind=p.kind AND m.source_id=p.source_id
 JOIN targets t ON t.kind=m.kind AND t.id=m.target_id
 WHERE p.source=? AND p.person_key=? AND c.expires_at>?
 ORDER BY CASE WHEN p.photo_path='' THEN 1 ELSE 0 END,p.kind,p.source_id LIMIT 1`, source, key, time.Now().Unix()).Scan(&person.ID, &person.Name, &person.PhotoPath)
	if err != nil {
		return person, err
	}
	person.Key = key
	if person.PhotoPath != "" {
		person.PhotoURL = "/api/people/" + source + "/" + key + "/photo"
	}
	return person, nil
}

func (a *App) person(w http.ResponseWriter, r *http.Request) {
	source, key, valid := personRequest(r)
	if !valid {
		fail(w, 400, "人物来源或标识无效")
		return
	}
	detail, err := a.personDetails(r.Context(), source, key)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "当前媒体库没有此人物的有效关联作品；资料可能已变更或到期")
		return
	}
	if err != nil {
		fail(w, 500, "无法读取人物关联作品")
		return
	}
	respond(w, 200, detail)
}

// Photos are lazy, authenticated and cached separately from title metadata.
// Never accept a URL from the caller, follow redirects, or forward credentials.
func (a *App) personPhoto(w http.ResponseWriter, r *http.Request) {
	source, key, valid := personRequest(r)
	if !valid {
		fail(w, 400, "人物来源或标识无效")
		return
	}
	person, err := a.personIdentity(r.Context(), source, key)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && person.PhotoPath == "") {
		fail(w, 404, "人物照片不可用")
		return
	}
	if err != nil {
		fail(w, 500, "无法读取人物照片关联")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	serveCached := func() bool {
		var photo []byte
		var typ string
		var expires, retry int64
		err := a.db.QueryRowContext(ctx, `SELECT photo,content_type,expires_at,retry_at FROM person_photos WHERE source=? AND photo_path=?`, source, person.PhotoPath).Scan(&photo, &typ, &expires, &retry)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			fail(w, 500, "无法读取人物照片")
			return true
		}
		if expires > time.Now().Unix() && len(photo) > 0 {
			respondPhoto(w, r, photo, typ)
			return true
		}
		if retry > time.Now().Unix() {
			fail(w, 404, "人物照片暂不可用，稍后重试")
			return true
		}
		return false
	}
	// Cached portraits never queue behind unrelated slow image downloads.
	if serveCached() {
		return
	}
	// Concurrent requests for the same uncached image share the persisted result.
	// The metadata worker has a separate lock and never waits for this gate.
	select {
	case a.metadata.photoGate <- struct{}{}:
		defer func() { <-a.metadata.photoGate }()
	case <-ctx.Done():
		fail(w, 503, "人物照片加载超时")
		return
	}
	if serveCached() {
		return
	}
	photo, typ, err := a.metadata.providers[source].photo(ctx, person.PhotoPath)
	typ = strings.Split(typ, ";")[0]
	if err != nil || len(photo) == 0 || (typ != "image/jpeg" && typ != "image/png" && typ != "image/webp") || http.DetectContentType(photo) != typ {
		if r.Context().Err() == nil && a.ctx.Err() == nil {
			_, _ = a.db.ExecContext(r.Context(), `INSERT INTO person_photos VALUES(?,?,NULL,'',0,?) ON CONFLICT(source,photo_path) DO UPDATE SET photo=NULL,content_type='',expires_at=0,retry_at=excluded.retry_at`, source, person.PhotoPath, time.Now().Add(24*time.Hour).Unix())
		}
		fail(w, 404, "人物照片暂不可用，使用占位图")
		return
	}
	_, err = a.db.ExecContext(ctx, `INSERT INTO person_photos VALUES(?,?,?,?,?,0) ON CONFLICT(source,photo_path) DO UPDATE SET photo=excluded.photo,content_type=excluded.content_type,expires_at=excluded.expires_at,retry_at=0`, source, person.PhotoPath, photo, typ, time.Now().Add(30*24*time.Hour).Unix())
	if err != nil {
		fail(w, 500, "无法缓存人物照片")
		return
	}
	respondPhoto(w, r, photo, typ)
}

func respondPhoto(w http.ResponseWriter, r *http.Request, photo []byte, typ string) {
	w.Header().Set("Content-Type", typ)
	w.Header().Set("Content-Length", strconv.Itoa(len(photo)))
	if r.Method != "HEAD" {
		_, _ = w.Write(photo)
	}
}
