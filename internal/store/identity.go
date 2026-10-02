package store

import (
	"context"
	"database/sql"
	"errors"
)

func (s *Store) Household(ctx context.Context) (string, []byte, error) {
	var name string
	var hash []byte
	err := s.reader.QueryRowContext(ctx, "SELECT username,password_hash FROM household WHERE singleton=1").Scan(&name, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, nil
	}
	return name, hash, err
}

// InitializeHousehold never replaces an existing account on restart.
func (s *Store) InitializeHousehold(ctx context.Context, name string, hash []byte) error {
	_, err := s.writer.ExecContext(ctx, "INSERT INTO household(singleton,username,password_hash) VALUES(1,?,?) ON CONFLICT(singleton) DO NOTHING", name, hash)
	return err
}
func (s *Store) CreateSession(ctx context.Context, hash, oldHash string, now, expires int64) error {
	tx, e := s.writer.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at<=? OR token_hash=?", now, oldHash); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO sessions(token_hash,expires_at) VALUES(?,?)", hash, expires); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Session(ctx context.Context, hash string, now int64) (string, error) {
	var name string
	e := s.reader.QueryRowContext(ctx, `SELECT h.username FROM sessions s JOIN household h ON h.singleton=s.household_id WHERE s.token_hash=? AND s.expires_at>?`, hash, now).Scan(&name)
	return name, e
}
func (s *Store) DeleteSession(ctx context.Context, hash string) error {
	_, e := s.writer.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash=?", hash)
	return e
}
