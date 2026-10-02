package store

import (
	"context"
	"database/sql"
)

// A scan publishes only its library. Every raw file has a row, even when normal
// browsing collapses it into a series. No duplicated remote content or progress.
func rebuildLocalProjection(ctx context.Context, tx *sql.Tx, library string) error {
	queries := []string{
		`DELETE FROM catalog_entries WHERE library_id=?`,
		`INSERT INTO catalog_entries(entry_key,library_id,media_file_id,subject_id,kind,local_title,local_search_key,title_sort_key,added_at,file_count,available_file_count)
 SELECT 'media:'||f.id,f.library_id,f.id,s.id,l.kind,rn_local_title(f.relative_path),rn_search_v1(f.relative_path),rn_title_key_v1(rn_local_title(f.relative_path)),f.added_at,1,(f.state='available')
 FROM media_files f JOIN libraries l ON l.id=f.library_id LEFT JOIN file_organization o ON o.media_file_id=f.id
 JOIN metadata_subjects s ON s.media_file_id=f.id OR s.series_id=o.series_id WHERE f.library_id=?`,
		`INSERT INTO catalog_entries(entry_key,library_id,series_id,subject_id,kind,local_title,local_search_key,title_sort_key,added_at,season_count,episode_count,file_count,available_file_count)
 SELECT 'series:'||se.id,se.library_id,se.id,s.id,'series',se.local_title,rn_search_v1(se.local_title),rn_title_key_v1(se.local_title),max(f.added_at),count(DISTINCT o.season_number),count(DISTINCT o.season_number||':'||o.episode_number),count(*),sum(f.state='available')
 FROM series se JOIN file_organization o ON o.series_id=se.id JOIN media_files f ON f.id=o.media_file_id JOIN metadata_subjects s ON s.series_id=se.id WHERE se.library_id=? GROUP BY se.id`,
	}
	for _, q := range queries {
		if _, e := tx.ExecContext(ctx, q, library); e != nil {
			return e
		}
	}
	return nil
}

// Deleting one file must not rebuild unrelated rows in that library. All counts
// and the catalog revision commit with the observable deletion state.
func updateDeletedProjection(ctx context.Context, tx *sql.Tx, id string) error {
	_, err := tx.ExecContext(ctx, `UPDATE catalog_entries SET available_file_count=0 WHERE media_file_id=?`, id)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE catalog_entries SET available_file_count=(SELECT count(*) FROM file_organization o JOIN media_files f ON f.id=o.media_file_id WHERE o.series_id=catalog_entries.series_id AND f.state='available') WHERE series_id=(SELECT series_id FROM file_organization WHERE media_file_id=?)`, id)
	return err
}
