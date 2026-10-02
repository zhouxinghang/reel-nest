import { useEffect, useState } from "react";
import { api, message } from "./api";
import type { CreditPerson, TitleMetadata } from "./media";
import { Poster } from "./metadata";
import { Icon } from "./icons";

export function PersonPhoto({ person }: { person: CreditPerson }) {
  const [failed, setFailed] = useState("");
  return (
    <div className="person-photo">
      {person.photoUrl && failed !== person.photoUrl ? (
        <img
          src={person.photoUrl}
          alt={`${person.name} 照片`}
          loading="lazy"
          onError={() => setFailed(person.photoUrl!)}
        />
      ) : (
        <div
          className="person-photo-placeholder"
          role="img"
          aria-label={`${person.name} 暂无照片`}
        >
          <Icon name="person" />
          <span>暂无照片</span>
        </div>
      )}
    </div>
  );
}

export function PersonCard({
  person,
  source,
  subtitle,
}: {
  person: CreditPerson;
  source: string;
  subtitle: string;
}) {
  return (
    <a
      className="person-card"
      href={`/people/${source.toLowerCase()}/${encodeURIComponent(person.key)}`}
      aria-label={`${person.name}，查看关联作品`}
    >
      <PersonPhoto person={person} />
      <div className="person-card-copy">
        <strong>{person.name}</strong>
        <span>{subtitle}</span>
      </div>
    </a>
  );
}

type PersonWork = {
  id: string;
  targetKind: "media" | "series";
  available: boolean;
  metadata: TitleMetadata;
  jobs: string[];
  characters: string[];
};
type PersonDetail = CreditPerson & {
  source: string;
  works: PersonWork[];
};

export function PersonDetails({
  source,
  personKey,
}: {
  source: string;
  personKey: string;
}) {
  const [person, setPerson] = useState<PersonDetail | null>(null);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const [filter, setFilter] = useState("all");
  useEffect(() => {
    let active = true;
    async function refresh() {
      try {
        const data = await api<PersonDetail>(
          `/api/people/${encodeURIComponent(source)}/${encodeURIComponent(personKey)}`,
        );
        if (active) {
          setPerson(data);
          setError("");
        }
      } catch (error) {
        if (active) {
          setPerson(null);
          setError(message(error));
        }
      }
    }
    setError("");
    void refresh();
    window.addEventListener("metadata-updated", refresh);
    return () => {
      active = false;
      window.removeEventListener("metadata-updated", refresh);
    };
  }, [source, personKey, attempt]);

  if (error)
    return (
      <section className="panel person-error">
        <h1>人物资料暂不可用</h1>
        <p className="error" role="alert">
          {error}
        </p>
        <button className="secondary" onClick={() => setAttempt(attempt + 1)}>
          重新加载
        </button>{" "}
        <a className="back-link" href="/library">
          返回媒体库
        </a>
      </section>
    );
  if (!person) return <p role="status">正在加载人物关联作品…</p>;
  const jobs = [...new Set(person.works.flatMap((work) => work.jobs))];
  const works = person.works.filter(
    (work) => filter === "all" || work.metadata.kind === filter,
  );
  return (
    <>
      <section className="person-hero" aria-label="人物详情">
        <PersonPhoto key={person.key} person={person} />
        <div>
          <p className="eyebrow">媒体库中的人物</p>
          <h1>{person.name}</h1>
          <p className="person-facts">
            {jobs.join(" · ")} <span>·</span> {person.source}
          </p>
          <p className="muted">已收录 {person.works.length} 部关联作品</p>
          <p className="fine">
            仅展示当前媒体库中已匹配的作品，不是完整作品年表。
          </p>
          {person.id === 0 && (
            <p className="notice">
              资料源未提供人物编号，仅展示当前作品，不按姓名合并其他作品。
            </p>
          )}
        </div>
      </section>
      <section className="detail-section person-works" aria-label="关联作品">
        <div className="person-works-heading">
          <h2>
            关联作品 <span className="count">{works.length}</span>
          </h2>
          <div className="person-filters" role="group" aria-label="作品类型">
            {(
              [
                ["all", "全部"],
                ["movie", "电影"],
                ["series", "电视剧"],
              ] as const
            ).map(([value, label]) => (
              <button
                key={value}
                className="secondary"
                aria-pressed={filter === value}
                onClick={() => setFilter(value)}
              >
                {label}
              </button>
            ))}
          </div>
        </div>
        {works.length === 0 ? (
          <p className="muted">暂无此类型的关联作品。</p>
        ) : (
          <ul className="person-work-grid">
            {works.map((work) => (
              <li key={`${work.metadata.kind}:${work.metadata.sourceId}`}>
                <a
                  className="person-work-card"
                  href={`/${work.targetKind}/${encodeURIComponent(work.id)}`}
                >
                  <Poster
                    metadata={work.metadata}
                    title={work.metadata.title}
                  />
                  <h3>{work.metadata.title}</h3>
                  <p>
                    {[
                      work.metadata.year,
                      work.metadata.kind === "movie" ? "电影" : "电视剧",
                    ]
                      .filter(Boolean)
                      .join(" · ")}
                  </p>
                  <p className="person-work-role">
                    {work.jobs.join(" / ")}
                    {work.characters.length > 0 &&
                      ` · 饰 ${work.characters.join(" / ")}`}
                  </p>
                  {!work.available && <p className="notice">文件不可用</p>}
                </a>
              </li>
            ))}
          </ul>
        )}
        <p className="fine">
          按资料源人物编号关联；不同资料源分别展示。电视剧按整部作品计数，不重复列出每一集。
        </p>
      </section>
    </>
  );
}
