export type WatchProgress = {
  position: number;
  duration: number;
  completed: boolean;
  updatedAt: number;
};

export type EpisodeReference = {
  seriesId: string;
  seriesTitle: string;
  seasonNumber: number;
  episodeNumber: number;
};

export type MediaFile = {
  match: MetadataAssociation;
  progress: WatchProgress | null;
  id: string;
  directoryId: string;
  directoryPath: string;
  filename: string;
  relativePath: string;
  path: string;
  kind: "movie" | "series";
  size: number;
  modifiedAt: number;
  available: boolean;
  matched: boolean;
  episode: EpisodeReference | null;
  organizationReason: string;
};

export type SeriesSummary = {
  match: MetadataAssociation;
  id: string;
  title: string;
  directoryId: string;
  directoryPath: string;
  seasonCount: number;
  episodeCount: number;
  fileCount: number;
  availableFileCount: number;
  matched: boolean;
};

export type SeriesDetail = SeriesSummary & {
  seasons: {
    number: number;
    episodes: { number: number; files: MediaFile[] }[];
  }[];
};

export type TitleMetadata = {
  source: "TMDB";
  tmdbId: number;
  kind: "movie" | "series";
  title: string;
  originalTitle: string;
  year: string;
  overview: string;
  posterUrl: string;
  sourceUrl: string;
  fetchedAt: number;
  expiresAt: number;
};
export type MetadataAssociation = {
  metadata: TitleMetadata | null;
  selection: string;
  status: string;
  error: string;
};
