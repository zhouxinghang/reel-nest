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
  addedAt: number;
  favorite: boolean;
  favoritedAt: number;
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
  addedAt: number;
  favorite: boolean;
  favoritedAt: number;
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

export type CreditPerson = {
  id: number;
  name: string;
  key: string;
  photoUrl?: string;
};

export type TitleMetadata = {
  source: "TMDB" | "TVDB" | "MetaTube";
  sourceId: number;
  tmdbId: number;
  kind: "movie" | "series";
  title: string;
  originalTitle: string;
  number?: string;
  provider?: string;
  year: string;
  overview: string;
  posterUrl: string;
  thumbUrl?: string;
  backdropUrl?: string;
  artwork?: {
    status: "available" | "failed";
    fetchedAt: number;
    error?: string;
  };
  sourceUrl: string;
  fetchedAt: number;
  expiresAt: number;
  credits?: {
    status: "available" | "failed";
    cast: (CreditPerson & { roles: string[] })[];
    directors: CreditPerson[];
    fetchedAt: number;
    retryAt?: number;
    error?: string;
    truncated?: boolean;
  };
};
export type MetadataAssociation = {
  metadata: TitleMetadata | null;
  selection: string;
  status: string;
  error: string;
};
