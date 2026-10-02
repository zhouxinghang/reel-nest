package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

type peopleIndexWriter interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type indexedPerson struct {
	CreditPerson
	Actor      bool
	Director   bool
	Characters []string
}

// The metadata payload remains the source of truth. This index only turns a
// person lookup from a full-library JSON scan into a bounded title lookup.
// State rows distinguish an indexed title with no credits from an old or
// externally modified payload that still needs rebuilding.
func (a *App) initializePeopleIndex() error {
	tx, err := a.db.BeginTx(a.ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(a.ctx, `
 CREATE TABLE IF NOT EXISTS metadata_people_state (
  source TEXT NOT NULL, kind TEXT NOT NULL, source_id INTEGER NOT NULL,
  PRIMARY KEY(source,kind,source_id),
  FOREIGN KEY(source,kind,source_id) REFERENCES metadata_cache(source,kind,source_id) ON DELETE CASCADE
 );
 CREATE TABLE IF NOT EXISTS metadata_people (
  source TEXT NOT NULL, kind TEXT NOT NULL, source_id INTEGER NOT NULL,
  person_key TEXT NOT NULL, person_id INTEGER NOT NULL, name TEXT NOT NULL,
  photo_path TEXT NOT NULL, actor INTEGER NOT NULL, director INTEGER NOT NULL,
  characters TEXT NOT NULL,
  PRIMARY KEY(source,kind,source_id,person_key),
  FOREIGN KEY(source,kind,source_id) REFERENCES metadata_people_state(source,kind,source_id) ON DELETE CASCADE
 );
 CREATE INDEX IF NOT EXISTS metadata_people_lookup ON metadata_people(source,person_key,kind,source_id);
 CREATE INDEX IF NOT EXISTS metadata_matches_source_title ON metadata_matches(source,kind,source_id,target_id);
 DROP TRIGGER IF EXISTS metadata_people_payload_changed;
 DROP TRIGGER IF EXISTS metadata_people_cache_deleted;
 CREATE TRIGGER metadata_people_payload_changed
 AFTER UPDATE OF payload ON metadata_cache
 WHEN OLD.payload <> NEW.payload
 BEGIN
  DELETE FROM metadata_people WHERE source=NEW.source AND kind=NEW.kind AND source_id=NEW.source_id;
  DELETE FROM metadata_people_state WHERE source=NEW.source AND kind=NEW.kind AND source_id=NEW.source_id;
 END;
 CREATE TRIGGER metadata_people_cache_deleted
 AFTER DELETE ON metadata_cache
 BEGIN
  DELETE FROM metadata_people WHERE source=OLD.source AND kind=OLD.kind AND source_id=OLD.source_id;
  DELETE FROM metadata_people_state WHERE source=OLD.source AND kind=OLD.kind AND source_id=OLD.source_id;
 END;`)
	if err != nil {
		return err
	}
	type cachedTitle struct {
		source, kind, payload string
		sourceID              int64
	}
	rows, err := tx.QueryContext(a.ctx, `SELECT c.source,c.kind,c.source_id,c.payload
 FROM metadata_cache c LEFT JOIN metadata_people_state s
 ON s.source=c.source AND s.kind=c.kind AND s.source_id=c.source_id
 WHERE s.source IS NULL ORDER BY c.source,c.kind,c.source_id`)
	if err != nil {
		return err
	}
	titles := []cachedTitle{}
	for rows.Next() {
		var title cachedTitle
		if err = rows.Scan(&title.source, &title.kind, &title.sourceID, &title.payload); err != nil {
			rows.Close()
			return err
		}
		titles = append(titles, title)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, title := range titles {
		var metadata TitleMetadata
		if json.Unmarshal([]byte(title.payload), &metadata) != nil {
			metadata = TitleMetadata{}
		}
		if err = replaceMetadataPeople(a.ctx, tx, title.source, title.kind, title.sourceID, metadata); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func metadataPeople(source, kind string, sourceID int64, metadata TitleMetadata) []indexedPerson {
	metadata.Source = strings.ToUpper(source)
	metadata.Kind = kind
	metadata.SourceID = sourceID
	decoratePeople(&metadata)
	if metadata.Credits == nil {
		return nil
	}
	people := []indexedPerson{}
	positions := map[string]int{}
	add := func(person CreditPerson) *indexedPerson {
		if position, ok := positions[person.Key]; ok {
			existing := &people[position]
			if existing.PhotoPath == "" && person.PhotoPath != "" {
				existing.CreditPerson = person
			}
			return existing
		}
		positions[person.Key] = len(people)
		people = append(people, indexedPerson{CreditPerson: person, Characters: []string{}})
		return &people[len(people)-1]
	}
	for _, member := range metadata.Credits.Cast {
		person := add(member.CreditPerson)
		person.Actor = true
		seen := map[string]bool{}
		for _, character := range person.Characters {
			seen[character] = true
		}
		for _, character := range member.Roles {
			if !seen[character] {
				seen[character] = true
				person.Characters = append(person.Characters, character)
			}
		}
	}
	for _, director := range metadata.Credits.Directors {
		add(director).Director = true
	}
	return people
}

func replaceMetadataPeople(ctx context.Context, writer peopleIndexWriter, source, kind string, sourceID int64, metadata TitleMetadata) error {
	if _, err := writer.ExecContext(ctx, `DELETE FROM metadata_people WHERE source=? AND kind=? AND source_id=?`, source, kind, sourceID); err != nil {
		return err
	}
	if _, err := writer.ExecContext(ctx, `DELETE FROM metadata_people_state WHERE source=? AND kind=? AND source_id=?`, source, kind, sourceID); err != nil {
		return err
	}
	if _, err := writer.ExecContext(ctx, `INSERT INTO metadata_people_state(source,kind,source_id) VALUES(?,?,?)`, source, kind, sourceID); err != nil {
		return err
	}
	for _, person := range metadataPeople(source, kind, sourceID, metadata) {
		characters, err := json.Marshal(person.Characters)
		if err != nil {
			return err
		}
		if _, err = writer.ExecContext(ctx, `INSERT INTO metadata_people(source,kind,source_id,person_key,person_id,name,photo_path,actor,director,characters) VALUES(?,?,?,?,?,?,?,?,?,?)`, source, kind, sourceID, person.Key, person.ID, person.Name, person.PhotoPath, person.Actor, person.Director, string(characters)); err != nil {
			return err
		}
	}
	return nil
}
