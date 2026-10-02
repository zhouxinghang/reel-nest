package model

import "errors"

var (
	ErrProvider  = errors.New("PROVIDER_UNAVAILABLE")
	ErrCandidate = errors.New("CANDIDATE_STALE")
	ErrMetadata  = errors.New("METADATA_UNAVAILABLE")
	ErrAsset     = errors.New("ASSET_UNAVAILABLE")
)

type RemoteRef struct {
	Source, CatalogID, Namespace, Kind, RemoteID string
	ProviderRevision                             int64
}
type Candidate struct {
	Ref                                        RemoteRef
	Title, OriginalTitle, Overview, Identifier string
	Year                                       *int
}
type SearchResult struct {
	Items    []Candidate
	Complete bool
}
type Credit struct {
	Key, Name, Role, Photo string
	Characters             []string
}

// Title is structured provider output. No persisted payload or local routes.
// Partial only replaces due optional parts, preserving the original TTL.
type Title struct {
	Ref                                                   RemoteRef
	Title, OriginalTitle, Identifier, Overview, SourceURL string
	Year                                                  *int
	FetchedAt, ExpiresAt, Revision                        int64
	CreditsState, ArtworkState                            string
	CreditsRetryAt, ArtworkRetryAt                        *int64
	Credits                                               []Credit
	Artwork                                               map[string]string
	Partial                                               bool
	UpdateCredits, UpdateArtwork                          bool
}
type ProviderVersion struct {
	Source, CatalogID string
	Revision          int64
}
type MetadataTarget struct {
	ID, LibraryID, Kind, Query                                   string
	SubjectRevision, BindingRevision, PolicyRevision, Generation int64
	Sources                                                      []string
	Selection, RemoteTitleID                                     string
	Ref                                                          RemoteRef
	Providers                                                    []ProviderVersion
}
type MetadataResult struct {
	Rebind               bool
	Target               MetadataTarget
	Title                *Title
	Selection, ErrorCode string
	RetryAt              int64
}
type StoredCredit struct {
	ID, PersonID, Name, Role string
	Characters               []string
	PhotoAssetID             *string
}
type Match struct {
	Target                   MetadataTarget
	CacheState, AttemptState string
	AttemptedAt              *int64
	ErrorCode                *string
	Title                    *Title
	Credits                  []StoredCredit
	Artwork                  map[string]string
}
type Asset struct {
	ID, RemoteTitleID, CatalogID, Source, Kind, Reference, State, MIME string
	Revision, ProviderRevision, SnapshotRevision, SnapshotExpiry       int64
	Bytes                                                              []byte
	ExpiresAt, RetryAt                                                 *int64
}
type Person struct {
	ID, Name, Source, CatalogID string
	PhotoAssetID                *string
}
type PersonWork struct {
	Filename                                                                      string
	FavoritedAt                                                                   *int64
	Progress                                                                      *Progress
	RemoteTitleID, EntryKey, MediaFileID, SeriesID, LibraryID, Kind, Title, State string
	AddedAt                                                                       int64
	SeasonCount, EpisodeCount, FileCount, AvailableFileCount                      int
	Favorite                                                                      bool
	Roles, Characters                                                             []string
	Match                                                                         Match
}
type MetadataJob struct {
	SubjectID, State                     string
	RequestedGeneration                  int64
	ClaimedGeneration                    *int64
	Attempts                             int
	RequestedAt                          int64
	StartedAt, FinishedAt, NextAttemptAt *int64
	ErrorCode                            *string
}
type MetadataStatus struct {
	Counts   map[string]int64
	Revision int64
	Failures []MetadataJob
}
