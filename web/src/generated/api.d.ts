/** Generated from api/openapi.yaml. Do not edit. */
export interface paths {
  "/healthz": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    /** @description Process liveness only, never configuration. */
    get: operations["health"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/readyz": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    /** @description DB and worker readiness, 503 NOT_READY when unavailable; no settings leaked. */
    get: operations["ready"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/api/login": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    get?: never;
    put?: never;
    /** @description login */
    post: operations["login"];
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/api/logout": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    get?: never;
    put?: never;
    /** @description logout */
    post: operations["logout"];
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/api/session": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    /** @description session */
    get: operations["session"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/api/libraries": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    /** @description libraries */
    get: operations["libraries"];
    put?: never;
    /** @description createLibrary */
    post: operations["createLibrary"];
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/api/libraries/{id}": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    get?: never;
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    /** @description updateLibrary */
    patch: operations["updateLibrary"];
    trace?: never;
  };
  "/api/libraries/{id}/scans": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    get?: never;
    put?: never;
    /** @description startScan */
    post: operations["startScan"];
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/api/scans/{id}": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    /** @description scan */
    get: operations["scan"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/api/home": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    /** @description home */
    get: operations["home"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/api/catalog/groups": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    /** @description Global browse/search/favorites grouped by library. recent/continue are ungrouped and rejected here. One page of group summaries, not all child entries. */
    get: operations["catalogGroups"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/api/catalog": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    /** @description Single-library entries, or global ungrouped recent/continue. Global browse/search/favorites use groups then independent libraryId pages. Organized episodes collapse only for browse/recent; unmatched includes raw organized files; favorites preserve independent media/series favorites. */
    get: operations["catalog"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/api/media/{id}": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    /** @description mediaDetail */
    get: operations["mediaDetail"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/api/series/{id}": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    /** @description seriesDetail */
    get: operations["seriesDetail"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/api/series/{id}/seasons/{number}/episodes": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    /** @description episodes */
    get: operations["episodes"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/api/favorites/{target}/{id}": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    get?: never;
    /** @description setFavorite */
    put: operations["setFavorite"];
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/api/metadata/subjects/{id}/candidates": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    /** @description candidates */
    get: operations["candidates"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/api/metadata/subjects/{id}/selection": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    get?: never;
    /** @description selectMetadata */
    put: operations["selectMetadata"];
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/api/metadata/refresh": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    get?: never;
    put?: never;
    /** @description refreshMetadata */
    post: operations["refreshMetadata"];
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/api/metadata/status": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    /** @description metadataStatus */
    get: operations["metadataStatus"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/api/settings/metadata": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    /** @description metadataSettings */
    get: operations["metadataSettings"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/api/settings/metadata/{source}": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    get?: never;
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    /** @description updateMetadataSettings */
    patch: operations["updateMetadataSettings"];
    trace?: never;
  };
  "/api/people/{id}": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    /** @description person */
    get: operations["person"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/api/people/{id}/works": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    /** @description personWorks */
    get: operations["personWorks"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/api/media/{id}/playback": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    /** @description playback */
    get: operations["playback"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/api/media/{id}/progress": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    /** @description progress */
    get: operations["progress"];
    /** @description saveProgress */
    put: operations["saveProgress"];
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/api/media/{id}/stream-link": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    get?: never;
    put?: never;
    /** @description streamLink */
    post: operations["streamLink"];
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/api/media/{id}/subtitles": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    /** @description subtitles */
    get: operations["subtitles"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/api/media/{id}/deletion-preview": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    get?: never;
    put?: never;
    /** @description deletionPreview */
    post: operations["deletionPreview"];
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/api/media/{id}/delete": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    get?: never;
    put?: never;
    /** @description deleteFile */
    post: operations["deleteFile"];
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/api/media/{id}/content": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    /** @description Verify file/parent identity then stream from the same handle, no transcoding or whole-file buffering. */
    get: operations["mediaContent"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    /** @description Verify file/parent identity then stream from the same handle, no transcoding or whole-file buffering. */
    head: operations["mediaContentHead"];
    patch?: never;
    trace?: never;
  };
  "/stream/{id}": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    /** @description Only token grants access to this file; Cookie never substitutes. No expiration, no progress writes. */
    get: operations["streamContent"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    /** @description Only token grants access to this file; Cookie never substitutes. No expiration, no progress writes. */
    head: operations["streamContentHead"];
    patch?: never;
    trace?: never;
  };
  "/api/assets/{id}/content": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    /** @description Only current valid bounded content, requires household session. No arbitrary URL proxy; cancellation is not negative cache. */
    get: operations["assetContent"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    /** @description Only current valid bounded content, requires household session. No arbitrary URL proxy; cancellation is not negative cache. */
    head: operations["assetContentHead"];
    patch?: never;
    trace?: never;
  };
  "/api/media/{id}/subtitles/{subtitle}": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    /** @description subtitleContent */
    get: operations["subtitleContent"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
}
export type webhooks = Record<string, never>;
export interface components {
  schemas: {
    /** @description Opaque local identifier, not a path or an upstream ID. All JSON IDs are strings. */
    ID: string;
    Revision: number;
    /** @description Unix milliseconds. Not seconds; zero is not an unknown-time sentinel. */
    Timestamp: number;
    /** @description Unix milliseconds, or null when the event has not happened. */
    NullableTimestamp: number | null;
    /** @enum {string} */
    Source: "tmdb" | "tvdb" | "metatube";
    /** @enum {string} */
    Kind: "movie" | "series";
    Empty: Record<string, never>;
    /** @enum {string} */
    ErrorCode:
      | "INVALID_REQUEST"
      | "UNAUTHENTICATED"
      | "FORBIDDEN"
      | "NOT_FOUND"
      | "RATE_LIMITED"
      | "REVISION_CONFLICT"
      | "LIBRARY_BUSY"
      | "SCAN_FAILED"
      | "SCAN_INTERRUPTED"
      | "LIBRARY_OVERLAP"
      | "INVALID_MEDIA_PATH"
      | "CURSOR_STALE"
      | "FILE_UNAVAILABLE"
      | "FILE_CHANGED"
      | "FILE_ACCESS_DENIED"
      | "SUBTITLE_INVALID"
      | "PROVIDER_UNAVAILABLE"
      | "CANDIDATE_STALE"
      | "METADATA_UNAVAILABLE"
      | "ASSET_UNAVAILABLE"
      | "DELETION_PREVIEW_EXPIRED"
      | "DELETION_TARGET_CHANGED"
      | "DATABASE_UNAVAILABLE"
      | "NOT_READY"
      | "INTERNAL_ERROR";
    Error: {
      error: {
        code: components["schemas"]["ErrorCode"];
        message: string;
        retryable: boolean;
        requestId: components["schemas"]["ID"];
      };
    };
    Health: {
      /** @enum {string} */
      status: "ok" | "not_ready";
    };
    Session: {
      username: string;
    };
    Login: {
      username: string;
      password: string;
    };
    OK: {
      /** @enum {boolean} */
      ok: true;
    };
    /** @description Keyset cursor TTL <= 5 minutes and no later than the nearest relevant metadata expiry. CURSOR_STALE on query/revision/TTL mismatch. Catalog revision for browsing, plus favorites/progress revision only for those views. */
    PageInfo: {
      nextCursor: string | null;
      expiresAt: components["schemas"]["NullableTimestamp"];
    };
    Favorite: {
      favorite: boolean;
      favoritedAt: components["schemas"]["NullableTimestamp"];
    };
    Progress: {
      position: number;
      duration: number;
      completed: boolean;
      updatedAt: components["schemas"]["Timestamp"];
    };
    /** @description Finite seconds; position <= duration. completed=true requires position >= duration - 0.5; browser sends it only after native ended. Last valid server-received update wins. */
    ProgressWrite: {
      position: number;
      duration: number;
      completed: boolean;
    };
    /** @description Ordered per-library priority. [] disables automatic fetching. No cross-source merging. */
    Sources: components["schemas"]["Source"][];
    ProviderStatus: {
      source: components["schemas"]["Source"];
      name: string;
      configured: boolean;
      cooldownUntil: components["schemas"]["NullableTimestamp"];
    };
    Library: {
      id: components["schemas"]["ID"];
      name: string;
      path: string;
      kind: components["schemas"]["Kind"];
      revision: components["schemas"]["Revision"];
      policyRevision: components["schemas"]["Revision"];
      sources: components["schemas"]["Sources"];
      createdAt: components["schemas"]["Timestamp"];
      latestScanId: string | null;
    };
    /** @description Trim name, 1–80 Unicode characters, no controls. Path must be inside configured roots, non-symlink, non-overlapping with libraries or DATA_DIR. Omitted sources defaults to [tmdb,tvdb]. Creates and queues first scan atomically. */
    LibraryCreate: {
      name: string;
      path: string;
      kind: components["schemas"]["Kind"];
      sources?: components["schemas"]["Sources"];
    };
    /** @description At least one changed field. Rename never modifies disk; sources affect automatic evaluation, not manual choice. Revision conflict is 409. */
    LibraryPatch: {
      revision: components["schemas"]["Revision"];
      name?: string;
      sources?: components["schemas"]["Sources"];
    };
    Scan: {
      id: components["schemas"]["ID"];
      libraryId: components["schemas"]["ID"];
      /** @enum {string} */
      state: "queued" | "running" | "completed" | "failed" | "interrupted";
      requestedAt: components["schemas"]["Timestamp"];
      startedAt: components["schemas"]["NullableTimestamp"];
      finishedAt: components["schemas"]["NullableTimestamp"];
      discoveredCount: number;
      errorCode: components["schemas"]["ErrorCode"] | null;
    };
    /** @description Upstream remoteId is an exact case-sensitive string, never hashed/coerced to integer. TMDB/TVDB namespace equals source. MetaTube namespace is normalized upstream provider, movie only; catalogId changes with normalized service URL, not token rotation. */
    RemoteRef: {
      source: components["schemas"]["Source"];
      catalogId: components["schemas"]["ID"];
      namespace: string;
      kind: components["schemas"]["Kind"];
      remoteId: string;
      providerRevision: components["schemas"]["Revision"];
    };
    Artwork: {
      primary: string | null;
      thumb: string | null;
      backdrop: string | null;
    };
    MetadataSummary: {
      title: string;
      originalTitle: string;
      year: number | null;
      source: components["schemas"]["Source"];
      artwork: components["schemas"]["Artwork"];
      expiresAt: components["schemas"]["Timestamp"];
    };
    EpisodePosition: {
      seriesId: components["schemas"]["ID"];
      seriesTitle: string;
      seasonNumber: number;
      episodeNumber: number;
    };
    MediaCard: {
      /**
       * @description discriminator enum property added by openapi-typescript
       * @enum {string}
       */
      entryType: "media";
      id: components["schemas"]["ID"];
      entryKey: string;
      libraryId: components["schemas"]["ID"];
      kind: components["schemas"]["Kind"];
      title: string;
      filename: string;
      /** @enum {string} */
      state: "available" | "deleted";
      addedAt: components["schemas"]["Timestamp"];
      favorite: components["schemas"]["Favorite"];
      progress: components["schemas"]["Progress"] | (never | null);
      episode: components["schemas"]["EpisodePosition"] | (never | null);
      metadata: components["schemas"]["MetadataSummary"] | (never | null);
    };
    SeriesCard: {
      /**
       * @description discriminator enum property added by openapi-typescript
       * @enum {string}
       */
      entryType: "series";
      id: components["schemas"]["ID"];
      entryKey: string;
      libraryId: components["schemas"]["ID"];
      /** @enum {string} */
      kind: "series";
      title: string;
      addedAt: components["schemas"]["Timestamp"];
      seasonCount: number;
      episodeCount: number;
      fileCount: number;
      availableFileCount: number;
      favorite: components["schemas"]["Favorite"];
      metadata: components["schemas"]["MetadataSummary"] | (never | null);
    };
    Card:
      | components["schemas"]["MediaCard"]
      | components["schemas"]["SeriesCard"];
    Credit: {
      personId: components["schemas"]["ID"];
      name: string;
      /** @enum {string} */
      role: "actor" | "director";
      characters: string[];
      photoAssetId: string | null;
    };
    Credits: {
      /** @enum {string} */
      state: "missing" | "ready" | "failed";
      actors: components["schemas"]["Credit"][];
      directors: components["schemas"]["Credit"][];
      retryAt: components["schemas"]["NullableTimestamp"];
    };
    MetadataDetail: {
      summary: components["schemas"]["MetadataSummary"];
      remote: components["schemas"]["RemoteRef"];
      identifier: string;
      overview: string;
      sourceUrl: string;
      fetchedAt: components["schemas"]["Timestamp"];
      revision: components["schemas"]["Revision"];
      credits: components["schemas"]["Credits"];
      /** @enum {string} */
      artworkState: "missing" | "ready" | "failed";
      artworkRetryAt: components["schemas"]["NullableTimestamp"];
    };
    Selection: {
      remote: components["schemas"]["RemoteRef"];
      /** @enum {string} */
      mode: "automatic" | "manual";
      revision: components["schemas"]["Revision"];
    };
    /** @description Selection survives absent/expired content. Only fresh metadata is returned; expired metadata is null. Organized episodes share their series subject. A failed refresh never clears manual selection. */
    Match: {
      subjectId: components["schemas"]["ID"];
      subjectRevision: components["schemas"]["Revision"];
      bindingRevision: number;
      selection: components["schemas"]["Selection"] | (never | null);
      /** @enum {string} */
      cacheState: "absent" | "fresh" | "expired";
      /** @enum {string} */
      attemptState: "never" | "succeeded" | "failed";
      attemptedAt: components["schemas"]["NullableTimestamp"];
      errorCode: components["schemas"]["ErrorCode"] | null;
      metadata: components["schemas"]["MetadataDetail"] | (never | null);
    };
    MediaDetail: {
      card: components["schemas"]["MediaCard"];
      relativePath: string;
      path: string;
      size: number;
      modifiedAt: number;
      organizationReason: string;
      match: components["schemas"]["Match"];
    };
    SeasonSummary: {
      number: number;
      episodeCount: number;
      fileCount: number;
    };
    /** @description Only season summaries, no embedded episode tree. Season summaries also page to avoid unbounded detail. */
    SeriesDetail: {
      card: components["schemas"]["SeriesCard"];
      match: components["schemas"]["Match"];
      seasons: components["schemas"]["SeasonSummary"][];
      page: components["schemas"]["PageInfo"];
    };
    /** @description One row per file version, ordered by episode number then stable file ID. Versions may span pages; never truncate a files array silently. */
    EpisodeFile: {
      episodeNumber: number;
      file: components["schemas"]["MediaCard"];
    };
    Person: {
      id: components["schemas"]["ID"];
      name: string;
      source: components["schemas"]["Source"];
      catalogId: components["schemas"]["ID"];
      photoAssetId: string | null;
    };
    /** @description Only current bindings + unexpired snapshots + local subjects. One representative per remote title, available files first; a series is not repeated per episode. */
    PersonWork: {
      card: components["schemas"]["Card"];
      roles: ("actor" | "director")[];
      characters: string[];
    };
    Candidate: {
      ref: components["schemas"]["RemoteRef"];
      title: string;
      originalTitle: string;
      year: number | null;
      overview: string;
    };
    /** @description Search is read-only: no persistent ID bridge. Incomplete/truncated results can never establish uniqueness. Candidate carries source/catalog/config revision. */
    CandidatePage: {
      items: components["schemas"]["Candidate"][];
      complete: boolean;
      subjectRevision: components["schemas"]["Revision"];
      bindingRevision: number;
    };
    /** @description expectedRevision=0 means no binding. Fetch and verify real upstream title before commit. Any subject/binding/policy/config/catalog mismatch returns CANDIDATE_STALE or REVISION_CONFLICT; failure retains previous selection. */
    SelectMetadata: {
      subjectRevision: components["schemas"]["Revision"];
      expectedRevision: number;
      ref: components["schemas"]["RemoteRef"];
    };
    /** @description Explicit refresh, optionally scoped to one library; not a filesystem scan. Valid caches retained; bounded retries and per-subject generation coalescing. */
    Refresh: {
      libraryId?: components["schemas"]["ID"];
    };
    RefreshAccepted: {
      queued: number;
    };
    Job: {
      subjectId: components["schemas"]["ID"];
      /** @enum {string} */
      state: "queued" | "running" | "succeeded" | "failed" | "superseded";
      requestedGeneration: components["schemas"]["Revision"];
      claimedGeneration: number | null;
      attempts: number;
      requestedAt: components["schemas"]["Timestamp"];
      startedAt: components["schemas"]["NullableTimestamp"];
      finishedAt: components["schemas"]["NullableTimestamp"];
      nextAttemptAt: components["schemas"]["NullableTimestamp"];
      errorCode: components["schemas"]["ErrorCode"] | null;
    };
    MetadataStatus: {
      sources: components["schemas"]["ProviderStatus"][];
      queued: number;
      running: number;
      succeeded: number;
      failed: number;
      superseded: number;
      revision: components["schemas"]["Revision"];
      failures: components["schemas"]["Job"][];
      page: components["schemas"]["PageInfo"];
    };
    /** @description No secrets or masked values. configured does not claim successful connectivity. */
    ProviderSettings: {
      source: components["schemas"]["Source"];
      name: string;
      url: string | null;
      catalogId: string | null;
      credentialSet: boolean;
      pinSet: boolean;
      configured: boolean;
      revision: components["schemas"]["Revision"];
    };
    /** @description Omitted/null/blank secrets preserve existing values; explicit clear deletes. Trimmed secret/PIN <=4096 UTF-8 bytes with no internal whitespace/control characters. Only TVDB accepts pin/clearPin, only MetaTube accepts url. URL <=2048 bytes, HTTP(S) with optional prefix, no userinfo/query/fragment. Changing address with an old token requires a replacement or explicit clear; cannot transfer token. MetaTube URLs are normalized to an explicit /v1 API suffix, with lowercase host and no default port or trailing slash; reverse-proxy prefix case is preserved. Null URL preserves; blank URL clears. Saving never waits for upstream requests. An unchanged save preserves the revision. */
    SettingsPatch: {
      revision: components["schemas"]["Revision"];
      credential?: string | null;
      clearCredential?: boolean;
      pin?: string | null;
      clearPin?: boolean;
      url?: string | null;
    };
    /** @description Preflight opens verified local file. Does not claim browser codec support or write progress. */
    Playback: {
      mediaId: components["schemas"]["ID"];
      contentPath: string;
      progress: components["schemas"]["Progress"] | (never | null);
    };
    /** @description Relative origin-independent path; non-expiring read-only single-file credential. No session fallback, subtitles or writes; not logged. */
    StreamLink: {
      path: string;
    };
    Subtitle: {
      id: components["schemas"]["ID"];
      filename: string;
      /** @enum {string} */
      format: "srt" | "vtt";
    };
    /** @description Signed short-lived exact file/parent identity material, not authorization. UI displays absolute path and irreversible risk before second confirmation. */
    DeletionPreview: {
      mediaId: components["schemas"]["ID"];
      path: string;
      confirmation: string;
      expiresAt: components["schemas"]["Timestamp"];
      warning: string;
    };
    DeleteRequest: {
      confirmation: string;
      /** @enum {boolean} */
      confirm: true;
    };
    /** @description HTTP 200 also covers deleted/index pending and unknown outcomes. Never auto-retry. Once unlink may have happened, use this outcome instead of an ordinary retryable error. Disconnect means unknown to client. Rescan/recheck; subtitles/images/directories untouched. */
    DeleteResult: {
      /** @enum {string} */
      fileOutcome: "deleted" | "not_deleted" | "unknown";
      /** @enum {string} */
      indexOutcome: "updated" | "pending";
      message: string;
      /** @enum {boolean} */
      retryable: false;
      requestId: components["schemas"]["ID"];
    };
    CatalogPage: {
      items: components["schemas"]["Card"][];
      page: components["schemas"]["PageInfo"];
    };
    EpisodePage: {
      items: components["schemas"]["EpisodeFile"][];
      page: components["schemas"]["PageInfo"];
    };
    PersonWorks: {
      items: components["schemas"]["PersonWork"][];
      page: components["schemas"]["PageInfo"];
    };
    LibrarySummary: {
      id: components["schemas"]["ID"];
      name: string;
      kind: components["schemas"]["Kind"];
    };
    LibraryPreview: {
      library: components["schemas"]["LibrarySummary"];
      items: components["schemas"]["Card"][];
    };
    LibraryPage: {
      items: components["schemas"]["Library"][];
      page: components["schemas"]["PageInfo"];
      allowedRoots: string[];
      sources: components["schemas"]["ProviderStatus"][];
    };
    /** @description Order derives from the first item of the complete matching set, then library ID, never from loaded child pages. Fetch entries independently by libraryId; entriesCursor binds group filters/revision. Empty libraries remain available in library/home pages. */
    Group: {
      library: components["schemas"]["LibrarySummary"];
      total: number;
      entriesCursor: string | null;
    };
    GroupPage: {
      items: components["schemas"]["Group"][];
      page: components["schemas"]["PageInfo"];
    };
    /** @description Recent is a single global top-20, series aggregate max current file addedAt; continue watching is per file. Empty libraries included. Cards omit credits, absolute paths, episode trees and BLOBs. Target <=256KiB. */
    Home: {
      libraries: components["schemas"]["LibraryPreview"][];
      librariesPage: components["schemas"]["PageInfo"];
      continueWatching: components["schemas"]["MediaCard"][];
      recent: components["schemas"]["Card"][];
    };
    FavoriteWrite: {
      favorite: boolean;
    };
    Settings: {
      sources: components["schemas"]["ProviderSettings"][];
    };
    SavedProgress: {
      progress: components["schemas"]["Progress"] | (never | null);
    };
    Subtitles: {
      items: components["schemas"]["Subtitle"][];
      page: components["schemas"]["PageInfo"];
    };
  };
  responses: {
    /** @description Stable error code, redacted message, explicit retryability and request correlation; no upstream payloads or secrets. */
    Error: {
      headers: {
        [name: string]: unknown;
      };
      content: {
        "application/json": components["schemas"]["Error"];
      };
    };
  };
  parameters: {
    /** @description Default 50, hard maximum 100; response may use a smaller bound. */
    Limit: number;
    Cursor: string;
    /** @description Case-insensitive substring of filename/local title/fresh metadata. Episode filename matches entire series without shrinking its counts. */
    Query: string;
    LibraryID: components["schemas"]["ID"];
    Kind: components["schemas"]["Kind"];
    View: "browse" | "unmatched" | "favorites" | "continue" | "recent";
    /** @description Server-stable Chinese/numeric title key or descending event time, then entryKey. Favorites/continue/recent use their event-time order. */
    Sort: "title" | "recent" | "favorite" | "progress";
  };
  requestBodies: never;
  headers: never;
  pathItems: never;
}
export type $defs = Record<string, never>;
export interface operations {
  health: {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Success */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["Health"];
        };
      };
      default: components["responses"]["Error"];
    };
  };
  ready: {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Success */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["Health"];
        };
      };
      default: components["responses"]["Error"];
    };
  };
  login: {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    requestBody: {
      content: {
        "application/json": components["schemas"]["Login"];
      };
    };
    responses: {
      /** @description Success */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["Session"];
        };
      };
      default: components["responses"]["Error"];
    };
  };
  logout: {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    requestBody: {
      content: {
        "application/json": components["schemas"]["Empty"];
      };
    };
    responses: {
      /** @description Success */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["OK"];
        };
      };
      default: components["responses"]["Error"];
    };
  };
  session: {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Success */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["Session"];
        };
      };
      default: components["responses"]["Error"];
    };
  };
  libraries: {
    parameters: {
      query?: {
        /** @description Default 50, hard maximum 100; response may use a smaller bound. */
        limit?: components["parameters"]["Limit"];
        cursor?: components["parameters"]["Cursor"];
      };
      header?: never;
      path?: never;
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Success */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["LibraryPage"];
        };
      };
      default: components["responses"]["Error"];
    };
  };
  createLibrary: {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    requestBody: {
      content: {
        "application/json": components["schemas"]["LibraryCreate"];
      };
    };
    responses: {
      /** @description Success */
      201: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["Library"];
        };
      };
      default: components["responses"]["Error"];
    };
  };
  updateLibrary: {
    parameters: {
      query?: never;
      header?: never;
      path: {
        id: components["schemas"]["ID"];
      };
      cookie?: never;
    };
    requestBody: {
      content: {
        "application/json": components["schemas"]["LibraryPatch"];
      };
    };
    responses: {
      /** @description Success */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["Library"];
        };
      };
      default: components["responses"]["Error"];
    };
  };
  startScan: {
    parameters: {
      query?: never;
      header?: never;
      path: {
        id: components["schemas"]["ID"];
      };
      cookie?: never;
    };
    requestBody: {
      content: {
        "application/json": components["schemas"]["Empty"];
      };
    };
    responses: {
      /** @description Success */
      202: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["Scan"];
        };
      };
      default: components["responses"]["Error"];
    };
  };
  scan: {
    parameters: {
      query?: never;
      header?: never;
      path: {
        id: components["schemas"]["ID"];
      };
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Success */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["Scan"];
        };
      };
      default: components["responses"]["Error"];
    };
  };
  home: {
    parameters: {
      query?: {
        cursor?: components["parameters"]["Cursor"];
      };
      header?: never;
      path?: never;
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Success */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["Home"];
        };
      };
      default: components["responses"]["Error"];
    };
  };
  catalogGroups: {
    parameters: {
      query?: {
        /** @description Default 50, hard maximum 100; response may use a smaller bound. */
        limit?: components["parameters"]["Limit"];
        cursor?: components["parameters"]["Cursor"];
        /** @description Case-insensitive substring of filename/local title/fresh metadata. Episode filename matches entire series without shrinking its counts. */
        q?: components["parameters"]["Query"];
        kind?: components["parameters"]["Kind"];
        view?: components["parameters"]["View"];
        /** @description Server-stable Chinese/numeric title key or descending event time, then entryKey. Favorites/continue/recent use their event-time order. */
        sort?: components["parameters"]["Sort"];
      };
      header?: never;
      path?: never;
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Success */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["GroupPage"];
        };
      };
      default: components["responses"]["Error"];
    };
  };
  catalog: {
    parameters: {
      query?: {
        /** @description Default 50, hard maximum 100; response may use a smaller bound. */
        limit?: components["parameters"]["Limit"];
        cursor?: components["parameters"]["Cursor"];
        /** @description Case-insensitive substring of filename/local title/fresh metadata. Episode filename matches entire series without shrinking its counts. */
        q?: components["parameters"]["Query"];
        libraryId?: components["parameters"]["LibraryID"];
        kind?: components["parameters"]["Kind"];
        view?: components["parameters"]["View"];
        /** @description Server-stable Chinese/numeric title key or descending event time, then entryKey. Favorites/continue/recent use their event-time order. */
        sort?: components["parameters"]["Sort"];
      };
      header?: never;
      path?: never;
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Success */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["CatalogPage"];
        };
      };
      default: components["responses"]["Error"];
    };
  };
  mediaDetail: {
    parameters: {
      query?: never;
      header?: never;
      path: {
        id: components["schemas"]["ID"];
      };
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Success */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["MediaDetail"];
        };
      };
      default: components["responses"]["Error"];
    };
  };
  seriesDetail: {
    parameters: {
      query?: {
        /** @description Default 50, hard maximum 100; response may use a smaller bound. */
        limit?: components["parameters"]["Limit"];
        cursor?: components["parameters"]["Cursor"];
      };
      header?: never;
      path: {
        id: components["schemas"]["ID"];
      };
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Success */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["SeriesDetail"];
        };
      };
      default: components["responses"]["Error"];
    };
  };
  episodes: {
    parameters: {
      query?: {
        /** @description Default 50, hard maximum 100; response may use a smaller bound. */
        limit?: components["parameters"]["Limit"];
        cursor?: components["parameters"]["Cursor"];
      };
      header?: never;
      path: {
        id: components["schemas"]["ID"];
        number: number;
      };
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Success */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["EpisodePage"];
        };
      };
      default: components["responses"]["Error"];
    };
  };
  setFavorite: {
    parameters: {
      query?: never;
      header?: never;
      path: {
        target: "media" | "series";
        id: components["schemas"]["ID"];
      };
      cookie?: never;
    };
    requestBody: {
      content: {
        "application/json": components["schemas"]["FavoriteWrite"];
      };
    };
    responses: {
      /** @description Success */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["Favorite"];
        };
      };
      default: components["responses"]["Error"];
    };
  };
  candidates: {
    parameters: {
      query: {
        q: string;
        source: components["schemas"]["Source"];
      };
      header?: never;
      path: {
        id: components["schemas"]["ID"];
      };
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Success */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["CandidatePage"];
        };
      };
      default: components["responses"]["Error"];
    };
  };
  selectMetadata: {
    parameters: {
      query?: never;
      header?: never;
      path: {
        id: components["schemas"]["ID"];
      };
      cookie?: never;
    };
    requestBody: {
      content: {
        "application/json": components["schemas"]["SelectMetadata"];
      };
    };
    responses: {
      /** @description Success */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["Match"];
        };
      };
      default: components["responses"]["Error"];
    };
  };
  refreshMetadata: {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    requestBody: {
      content: {
        "application/json": components["schemas"]["Refresh"];
      };
    };
    responses: {
      /** @description Success */
      202: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["RefreshAccepted"];
        };
      };
      default: components["responses"]["Error"];
    };
  };
  metadataStatus: {
    parameters: {
      query?: {
        libraryId?: components["parameters"]["LibraryID"];
        /** @description Default 50, hard maximum 100; response may use a smaller bound. */
        limit?: components["parameters"]["Limit"];
        cursor?: components["parameters"]["Cursor"];
      };
      header?: never;
      path?: never;
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Success */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["MetadataStatus"];
        };
      };
      default: components["responses"]["Error"];
    };
  };
  metadataSettings: {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Success */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["Settings"];
        };
      };
      default: components["responses"]["Error"];
    };
  };
  updateMetadataSettings: {
    parameters: {
      query?: never;
      header?: never;
      path: {
        source: components["schemas"]["Source"];
      };
      cookie?: never;
    };
    requestBody: {
      content: {
        "application/json": components["schemas"]["SettingsPatch"];
      };
    };
    responses: {
      /** @description Success */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["ProviderSettings"];
        };
      };
      default: components["responses"]["Error"];
    };
  };
  person: {
    parameters: {
      query?: never;
      header?: never;
      path: {
        id: components["schemas"]["ID"];
      };
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Success */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["Person"];
        };
      };
      default: components["responses"]["Error"];
    };
  };
  personWorks: {
    parameters: {
      query?: {
        /** @description Default 50, hard maximum 100; response may use a smaller bound. */
        limit?: components["parameters"]["Limit"];
        cursor?: components["parameters"]["Cursor"];
      };
      header?: never;
      path: {
        id: components["schemas"]["ID"];
      };
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Success */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["PersonWorks"];
        };
      };
      default: components["responses"]["Error"];
    };
  };
  playback: {
    parameters: {
      query?: never;
      header?: never;
      path: {
        id: components["schemas"]["ID"];
      };
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Success */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["Playback"];
        };
      };
      default: components["responses"]["Error"];
    };
  };
  progress: {
    parameters: {
      query?: never;
      header?: never;
      path: {
        id: components["schemas"]["ID"];
      };
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Success */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["SavedProgress"];
        };
      };
      default: components["responses"]["Error"];
    };
  };
  saveProgress: {
    parameters: {
      query?: never;
      header?: never;
      path: {
        id: components["schemas"]["ID"];
      };
      cookie?: never;
    };
    requestBody: {
      content: {
        "application/json": components["schemas"]["ProgressWrite"];
      };
    };
    responses: {
      /** @description Success */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["Progress"];
        };
      };
      default: components["responses"]["Error"];
    };
  };
  streamLink: {
    parameters: {
      query?: never;
      header?: never;
      path: {
        id: components["schemas"]["ID"];
      };
      cookie?: never;
    };
    requestBody: {
      content: {
        "application/json": components["schemas"]["Empty"];
      };
    };
    responses: {
      /** @description Success */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["StreamLink"];
        };
      };
      default: components["responses"]["Error"];
    };
  };
  subtitles: {
    parameters: {
      query?: {
        /** @description Default 50, hard maximum 100; response may use a smaller bound. */
        limit?: components["parameters"]["Limit"];
        cursor?: components["parameters"]["Cursor"];
      };
      header?: never;
      path: {
        id: components["schemas"]["ID"];
      };
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Success */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["Subtitles"];
        };
      };
      default: components["responses"]["Error"];
    };
  };
  deletionPreview: {
    parameters: {
      query?: never;
      header?: never;
      path: {
        id: components["schemas"]["ID"];
      };
      cookie?: never;
    };
    requestBody: {
      content: {
        "application/json": components["schemas"]["Empty"];
      };
    };
    responses: {
      /** @description Success */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["DeletionPreview"];
        };
      };
      default: components["responses"]["Error"];
    };
  };
  deleteFile: {
    parameters: {
      query?: never;
      header?: never;
      path: {
        id: components["schemas"]["ID"];
      };
      cookie?: never;
    };
    requestBody: {
      content: {
        "application/json": components["schemas"]["DeleteRequest"];
      };
    };
    responses: {
      /** @description Success */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["DeleteResult"];
        };
      };
      default: components["responses"]["Error"];
    };
  };
  mediaContent: {
    parameters: {
      query?: never;
      header?: {
        Range?: string;
        "If-Range"?: string;
      };
      path: {
        id: components["schemas"]["ID"];
      };
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Content */
      200: {
        headers: {
          "Content-Type"?: string;
          "Content-Length"?: number;
          ETag?: string;
          "Accept-Ranges"?: "bytes";
          [name: string]: unknown;
        };
        content: {
          "video/*": string;
          "application/octet-stream": string;
        };
      };
      /** @description Single or multipart byte range */
      206: {
        headers: {
          "Content-Range"?: string;
          [name: string]: unknown;
        };
        content: {
          "video/*": string;
          "application/octet-stream": string;
          "multipart/byteranges": string;
        };
      };
      /** @description Unsatisfiable or malformed range; standard HTTP text permitted */
      416: {
        headers: {
          "Content-Range"?: string;
          [name: string]: unknown;
        };
        content: {
          "text/plain": string;
        };
      };
      default: components["responses"]["Error"];
    };
  };
  mediaContentHead: {
    parameters: {
      query?: never;
      header?: {
        Range?: string;
        "If-Range"?: string;
      };
      path: {
        id: components["schemas"]["ID"];
      };
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Same headers, no body */
      200: {
        headers: {
          "Content-Type"?: string;
          "Content-Length"?: number;
          ETag?: string;
          "Accept-Ranges"?: "bytes";
          [name: string]: unknown;
        };
        content?: never;
      };
      /** @description Single or multipart byte range */
      206: {
        headers: {
          "Content-Range"?: string;
          [name: string]: unknown;
        };
        content?: never;
      };
      /** @description Unsatisfiable or malformed range; standard HTTP text permitted */
      416: {
        headers: {
          "Content-Range"?: string;
          [name: string]: unknown;
        };
        content: {
          "text/plain": string;
        };
      };
      default: components["responses"]["Error"];
    };
  };
  streamContent: {
    parameters: {
      query: {
        token: string;
      };
      header?: {
        Range?: string;
        "If-Range"?: string;
      };
      path: {
        id: components["schemas"]["ID"];
      };
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Content */
      200: {
        headers: {
          "Content-Type"?: string;
          "Content-Length"?: number;
          ETag?: string;
          "Accept-Ranges"?: "bytes";
          [name: string]: unknown;
        };
        content: {
          "video/*": string;
          "application/octet-stream": string;
        };
      };
      /** @description Single or multipart byte range */
      206: {
        headers: {
          "Content-Range"?: string;
          [name: string]: unknown;
        };
        content: {
          "video/*": string;
          "application/octet-stream": string;
          "multipart/byteranges": string;
        };
      };
      /** @description Unsatisfiable or malformed range; standard HTTP text permitted */
      416: {
        headers: {
          "Content-Range"?: string;
          [name: string]: unknown;
        };
        content: {
          "text/plain": string;
        };
      };
      default: components["responses"]["Error"];
    };
  };
  streamContentHead: {
    parameters: {
      query: {
        token: string;
      };
      header?: {
        Range?: string;
        "If-Range"?: string;
      };
      path: {
        id: components["schemas"]["ID"];
      };
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Same headers, no body */
      200: {
        headers: {
          "Content-Type"?: string;
          "Content-Length"?: number;
          ETag?: string;
          "Accept-Ranges"?: "bytes";
          [name: string]: unknown;
        };
        content?: never;
      };
      /** @description Single or multipart byte range */
      206: {
        headers: {
          "Content-Range"?: string;
          [name: string]: unknown;
        };
        content?: never;
      };
      /** @description Unsatisfiable or malformed range; standard HTTP text permitted */
      416: {
        headers: {
          "Content-Range"?: string;
          [name: string]: unknown;
        };
        content: {
          "text/plain": string;
        };
      };
      default: components["responses"]["Error"];
    };
  };
  assetContent: {
    parameters: {
      query?: never;
      header?: never;
      path: {
        id: components["schemas"]["ID"];
      };
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Content */
      200: {
        headers: {
          "Content-Type"?: string;
          "Content-Length"?: number;
          [name: string]: unknown;
        };
        content: {
          "image/jpeg": string;
          "image/png": string;
          "image/webp": string;
          "image/gif": string;
        };
      };
      default: components["responses"]["Error"];
    };
  };
  assetContentHead: {
    parameters: {
      query?: never;
      header?: never;
      path: {
        id: components["schemas"]["ID"];
      };
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Same headers, no body */
      200: {
        headers: {
          "Content-Type"?: string;
          "Content-Length"?: number;
          [name: string]: unknown;
        };
        content?: never;
      };
      default: components["responses"]["Error"];
    };
  };
  subtitleContent: {
    parameters: {
      query?: never;
      header?: never;
      path: {
        id: components["schemas"]["ID"];
        subtitle: components["schemas"]["ID"];
      };
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Validated UTF-8 WebVTT, <=2MiB source. No source writes. */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "text/vtt": string;
        };
      };
      default: components["responses"]["Error"];
    };
  };
}
