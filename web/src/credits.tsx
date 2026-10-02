import { useState } from "react";
import type { TitleMetadata } from "./media";
import { PersonCard } from "./people";

export function CreditsInfo({ metadata }: { metadata?: TitleMetadata | null }) {
  const [expanded, setExpanded] = useState(false);
  const credits = metadata?.credits;
  const cast = credits?.cast ?? [];
  const directors = credits?.directors ?? [];
  return (
    <section className="detail-section title-credits" aria-label="演职员">
      <div className="credits-heading">
        <h2>演职员</h2>
        {metadata && (
          <span className="fine">
            {metadata.source} ·{" "}
            {metadata.kind === "series"
              ? "整部电视剧资料，非本集专属"
              : "电影资料"}
          </span>
        )}
      </div>
      {!metadata ? (
        <p className="fine">匹配影片资料后可查看演员和导演。</p>
      ) : !credits ? (
        <p className="fine">
          演职员信息待补齐；启用对应资料源后，在后台匹配时获取。
        </p>
      ) : credits.status === "failed" ? (
        <p className="notice" role="status">
          演职员信息暂时无法获取：{credits.error}。已有影片资料与播放不受影响。
          {credits.retryAt && (
            <>
              可在 {new Date(credits.retryAt * 1000).toLocaleString()}{" "}
              后通过“匹配 / 刷新到期资料”重试。
            </>
          )}
        </p>
      ) : (
        <>
          <div className="credits-directors">
            <h3>导演</h3>
            {directors.length ? (
              <ul className="credits-cast" aria-label="导演">
                {directors.map((person, index) => (
                  <li key={`${person.id}:${index}`}>
                    <PersonCard
                      person={person}
                      source={metadata.source}
                      subtitle="导演"
                    />
                  </li>
                ))}
              </ul>
            ) : (
              <p className="fine">暂无导演信息</p>
            )}
          </div>
          <h3>演员</h3>
          {cast.length ? (
            <ul className="credits-cast" aria-label="演员">
              {(expanded ? cast : cast.slice(0, 8)).map((person, index) => (
                <li key={`${person.id}:${index}`}>
                  <PersonCard
                    person={person}
                    source={metadata.source}
                    subtitle={
                      person.roles.length
                        ? `饰 ${person.roles.join(" / ")}`
                        : "角色未提供"
                    }
                  />
                </li>
              ))}
            </ul>
          ) : (
            <p className="fine">暂无演员信息</p>
          )}
          {cast.length > 8 && (
            <button
              className="text-button credits-toggle"
              aria-expanded={expanded}
              onClick={() => setExpanded(!expanded)}
            >
              {expanded ? "收起演员" : `展开全部 ${cast.length} 位演员`}
            </button>
          )}
          {credits.truncated && (
            <p className="fine">
              名单较长，仅保留前 100 位演员和前 50
              位导演；完整名单请查看资料来源。
            </p>
          )}
          {credits.error && (
            <p className="notice" role="status">
              人物照片资料暂未补齐，已有演职员保留。{credits.error}
            </p>
          )}
        </>
      )}
    </section>
  );
}
