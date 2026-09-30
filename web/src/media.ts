export type EpisodeReference = {
  seriesId: string;
  seriesTitle: string;
  seasonNumber: number;
  episodeNumber: number;
};

export type MediaFile = {
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
