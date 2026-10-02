import type { ReactNode } from "react";
import type { MetadataAssociation } from "./media";
import { DetailBackdrop, MetadataInfo, Poster } from "./metadata";
import { CreditsInfo } from "./credits";

/** Shared, data-backed presentation for movies, series and individual files. */
export function DetailHero({
  title,
  posterTitle = title,
  match,
  kind,
  facts,
  children,
}: {
  title: string;
  posterTitle?: string;
  match: MetadataAssociation;
  kind: string;
  facts?: ReactNode;
  children: ReactNode;
}) {
  const metadata = match.metadata;
  return (
    <>
      <DetailBackdrop
        key={`${metadata?.source}:${metadata?.sourceId}:${metadata?.fetchedAt}`}
        metadata={metadata}
      />
      <section className="detail-hero" aria-label="影片详情">
        <Poster detail metadata={metadata} title={posterTitle} />
        <div className="detail-summary">
          <h1>{title}</h1>
          {metadata?.originalTitle && metadata.originalTitle !== title && (
            <p className="detail-original-title">{metadata.originalTitle}</p>
          )}
          <div className="detail-facts">
            {metadata?.year && <span>{metadata.year}</span>}
            <span>{kind}</span>
            {facts}
            {!metadata && <span className="detail-tag">未匹配媒体</span>}
          </div>
          {children}
          <MetadataInfo
            key={
              metadata ? `${metadata.source}:${metadata.sourceId}` : "unmatched"
            }
            match={match}
          />
        </div>
      </section>
      <CreditsInfo
        key={metadata ? `${metadata.source}:${metadata.sourceId}` : "unmatched"}
        metadata={metadata}
      />
    </>
  );
}

export function fileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let value = bytes / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  return `${value.toLocaleString(undefined, { maximumFractionDigits: 1 })} ${units[unit]}`;
}
