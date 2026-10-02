// Package model contains shared values only; it performs no I/O.
package model

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
)

var (
	ErrNotFound = errors.New("NOT_FOUND")
	ErrInvalid  = errors.New("INVALID_REQUEST")
	ErrConflict = errors.New("REVISION_CONFLICT")
	ErrBusy     = errors.New("LIBRARY_BUSY")
	ErrClosed   = errors.New("NOT_READY")
	ErrCursor   = errors.New("CURSOR_STALE")
	ErrOverlap  = errors.New("LIBRARY_OVERLAP")
)

func NewID() string { var b [16]byte; _, _ = rand.Read(b[:]); return hex.EncodeToString(b[:]) }

type Library struct {
	ID, Name, Path, Identity, Kind      string
	Revision, PolicyRevision, CreatedAt int64
	Sources                             []string
	LatestScanID                        *string
}
type Scan struct {
	ID, LibraryID, State  string
	RequestedAt           int64
	StartedAt, FinishedAt *int64
	DiscoveredCount       int64
	ErrorCode             *string
}
type Organization struct {
	Title, Key, Reason string
	Season, Episode    int
}
type ScanEntry struct {
	Relative, Fingerprint string
	Size, ModifiedAt      int64
	Organization          Organization
}
type MediaFile struct {
	ID, LibraryID, Relative, Fingerprint, State, Reason string
	RootPath, RootIdentity, Kind                        string
	Size, ModifiedAt, AddedAt                           int64
	SeriesID, SeriesTitle, SubjectID                    string
	Season, Episode                                     int
}
type Progress struct {
	Position, Duration float64
	Completed          bool
	UpdatedAt          int64
}
