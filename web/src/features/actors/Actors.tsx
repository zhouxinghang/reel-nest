import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router-dom";
import { useAPI } from "../../shared/services";
import { Loading, QueryError, ui } from "../../shared/ui";
import { queries } from "../catalog/queries";
import styles from "./actors.module.css";

const imageURL = (id: string) =>
  `/api/metadata/actor-images/${encodeURIComponent(id)}`;

function ActorImage({ id, alt }: { id: string; alt: string }) {
  const [failed, setFailed] = useState(false);
  return failed ? (
    <div className={styles.placeholder}>
      <span>图片暂不可用</span>
      <button className={ui.button} onClick={() => setFailed(false)}>
        重试图片
      </button>
    </div>
  ) : (
    <img
      src={imageURL(id)}
      alt={alt}
      loading="lazy"
      onError={() => setFailed(true)}
    />
  );
}

function SearchForm({ q, submit }: { q: string; submit: (q: string) => void }) {
  const [name, setName] = useState(q);
  return (
    <form
      className={styles.search}
      role="search"
      aria-label="查询演员"
      onSubmit={(e) => {
        e.preventDefault();
        if (name.trim()) submit(name.trim());
      }}
    >
      <label>
        演员名字
        <input
          className={ui.input}
          value={name}
          maxLength={256}
          onChange={(e) => setName(e.target.value)}
          placeholder="输入演员姓名或别名"
          required
        />
      </label>
      <button className={ui.primary} disabled={!name.trim()}>
        查询演员
      </button>
    </form>
  );
}

function ActorProfile({ id }: { id: string }) {
  const api = useAPI();
  const query = useQuery({
    queryKey: ["actor-detail", id],
    gcTime: 0,
    retry: false,
    queryFn: ({ signal }) =>
      api("get", "/api/metadata/actors/{id}", {
        path: { id },
        signal,
        timeoutMs: 25000,
      }),
  });
  const a = query.data;
  return (
    <section aria-label="演员资料" className={styles.profile}>
      <QueryError error={query.error} retry={() => void query.refetch()} />
      {query.error && <p>候选过期或配置变化时，请重新搜索后选择演员。</p>}
      {query.isPending && <Loading />}
      {a && !query.error && (
        <>
          <div className={styles.hero}>
            <div className={styles.portrait}>
              <ActorImage
                key={a.photoId}
                id={a.photoId}
                alt={`${a.name}头像`}
              />
            </div>
            <div>
              <h2>{a.name}</h2>
              <p className={ui.muted}>MetaTube · {a.provider}</p>
              {a.aliases.length > 0 && <p>别名：{a.aliases.join(" / ")}</p>}
              {a.homepage && (
                <a href={a.homepage} target="_blank" rel="noreferrer">
                  来源主页 ↗
                </a>
              )}
            </div>
          </div>
          <p className={styles.summary}>{a.summary || "暂无演员简介"}</p>
          <dl className={styles.facts}>
            {[
              ["生日", a.birthday],
              ["出道日期", a.debutDate],
              ["国籍", a.nationality],
              ["身高", a.height ? `${a.height} cm` : ""],
              ["血型", a.bloodType],
              ["爱好", a.hobby],
              ["特长", a.skill],
              ["三围", a.measurements],
              ["罩杯", a.cupSize],
            ].map(([label, value]) =>
              value ? (
                <div key={label}>
                  <dt>{label}</dt>
                  <dd>{value}</dd>
                </div>
              ) : null,
            )}
          </dl>
          <h3>图片与壁纸</h3>
          <p className={ui.muted}>
            保留原始比例，不裁剪或拉伸；图片数量和清晰度取决于资料源。
          </p>
          <div className={styles.gallery}>
            {a.wallpapers.map((image, index) => (
              <figure key={image}>
                <ActorImage id={image} alt={`${a.name}壁纸 ${index + 1}`} />
                <figcaption>
                  <a href={imageURL(image)} target="_blank" rel="noreferrer">
                    查看大图 {index + 1} ↗
                  </a>
                  <a
                    href={imageURL(image)}
                    download={`${a.name}-壁纸-${index + 1}`}
                  >
                    下载图片
                  </a>
                </figcaption>
              </figure>
            ))}
          </div>
        </>
      )}
    </section>
  );
}

export function Actors() {
  const api = useAPI();
  const client = useQueryClient();
  const [search, setSearch] = useSearchParams();
  const q = (search.get("q") || "").trim();
  const selected = search.get("actor") || "";
  const settings = useQuery(queries.settings(api));
  const configured = settings.data?.sources.find(
    (s) => s.source === "metatube",
  )?.configured;
  const query = useQuery({
    queryKey: ["actor-search", q],
    enabled: !!q && configured === true,
    gcTime: 0,
    retry: false,
    queryFn: ({ signal }) =>
      api("get", "/api/metadata/actors/search", {
        query: { q },
        signal,
        timeoutMs: 25000,
      }),
  });
  function submit(name: string) {
    setSearch({ q: name });
    void client.invalidateQueries({ queryKey: ["actor-search", name] });
  }
  return (
    <>
      <h1>演员查询</h1>
      <p className={ui.muted}>
        按名字查询 MetaTube
        演员资料与壁纸，无需先添加影片。同名结果请核对来源；不会自动合并本地人物或修改影片资料。
      </p>
      <QueryError
        error={settings.error}
        retry={() => void settings.refetch()}
      />
      {settings.isPending && <Loading />}
      {configured === false && (
        <p>
          尚未配置 MetaTube，请先到{" "}
          <Link to="/settings/metadata">影片资料源</Link> 填写服务地址和所需
          Token。
        </p>
      )}
      {configured && (
        <>
          <SearchForm key={q} q={q} submit={submit} />
          <QueryError error={query.error} retry={() => void query.refetch()} />
          {q && query.isFetching && <Loading />}
          {query.data && !query.error && (
            <section aria-label="演员搜索结果">
              <p role="status">
                {query.data.items.length
                  ? `找到 ${query.data.items.length} 个候选，请选择演员`
                  : "未找到演员，请尝试其他名字或别名。"}
              </p>
              {query.data.truncated && (
                <p>结果过多或存在无效条目，请使用更准确的姓名缩小范围。</p>
              )}
              <div className={styles.results}>
                {query.data.items.map((a) => (
                  <button
                    key={a.id}
                    className={styles.candidate}
                    aria-label={`选择演员 ${a.name} · ${a.provider}`}
                    aria-pressed={selected === a.id}
                    onClick={() => setSearch({ q, actor: a.id })}
                  >
                    <div className={styles.thumbnail}>
                      <img
                        src={imageURL(a.photoId)}
                        alt=""
                        loading="lazy"
                        onError={(e) => (e.currentTarget.hidden = true)}
                      />
                    </div>
                    <span>
                      <strong>{a.name}</strong>
                      <span className={ui.muted}>{a.provider}</span>
                      {a.aliases.length > 0 && (
                        <span>{a.aliases.join(" / ")}</span>
                      )}
                    </span>
                  </button>
                ))}
              </div>
            </section>
          )}
          {selected && (
            <>
              <button className={ui.button} onClick={() => submit(q)}>
                重新搜索
              </button>
              <ActorProfile key={selected} id={selected} />
            </>
          )}
        </>
      )}
    </>
  );
}
