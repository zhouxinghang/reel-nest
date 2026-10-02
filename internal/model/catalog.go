package model

// CatalogCard is a light read model; Match contains only a summary for lists.
type CatalogCard struct {
	Filename                                                                      string
	FavoritedAt                                                                   *int64
	Progress                                                                      *Progress
	RemoteTitleID, EntryKey, MediaFileID, SeriesID, LibraryID, Kind, Title, State string
	SubjectID, ParentSeriesID, ParentSeriesTitle                                  string
	Season, Episode                                                               int
	AddedAt                                                                       int64
	SeasonCount, EpisodeCount, FileCount, AvailableFileCount                      int
	Favorite                                                                      bool
	Match                                                                         Match
}
type CatalogQuery struct{ LibraryID, Kind, View, Sort, Query string }
type CatalogPosition struct{ Key, ID string }
type CatalogPage struct {
	Items             []CatalogCard
	Revision, Expires int64
	Last              CatalogPosition
	More              bool
}
type CatalogGroup struct {
	Library  Library
	Total    int64
	Position CatalogPosition
}
type CatalogGroups struct {
	Items             []CatalogGroup
	Revision, Expires int64
	More              bool
}
type MediaDetail struct {
	Card  CatalogCard
	File  MediaFile
	Match Match
}
type SeasonSummary struct{ Number, EpisodeCount, FileCount int }
type SeriesDetail struct {
	Card              CatalogCard
	Match             Match
	Seasons           []SeasonSummary
	Revision, Expires int64
	More              bool
}
type LibraryPreview struct {
	Library Library
	Items   []CatalogCard
}
type Home struct {
	Libraries         []LibraryPreview
	Continue, Recent  []CatalogCard
	Revision, Expires int64
	More              bool
}
