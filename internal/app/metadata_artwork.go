package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Primary is the existing poster. Thumb and Backdrop are independently selected
// landscape artwork, never a portrait stretched to fit a different surface.
type TitleArtwork struct {
	Version      int    `json:"version"`
	Status       string `json:"status"`
	ThumbPath    string `json:"thumbPath,omitempty"`
	BackdropPath string `json:"backdropPath,omitempty"`
	FetchedAt    int64  `json:"fetchedAt"`
	RetryAt      int64  `json:"retryAt,omitempty"`
	Error        string `json:"error,omitempty"`
}

func artworkDue(art *TitleArtwork) bool {
	return art == nil || ((art.Version < 1 || art.Status == "failed") && art.RetryAt <= time.Now().Unix())
}

func fetchTitleArtwork(ctx context.Context, client metadataProvider, kind string, id int64, previous *TitleArtwork) *TitleArtwork {
	art, err := client.artwork(ctx, kind, id)
	art.FetchedAt = time.Now().Unix()
	if err != nil {
		art = TitleArtwork{Status: "failed", FetchedAt: art.FetchedAt, RetryAt: time.Now().Add(24 * time.Hour).Unix(), Error: err.Error()}
		if previous != nil {
			art.ThumbPath, art.BackdropPath = previous.ThumbPath, previous.BackdropPath
		}
	} else {
		art.Version, art.Status = 1, "available"
	}
	return &art
}

func decorateArtwork(m *TitleMetadata) {
	m.ThumbURL, m.BackdropURL = "", ""
	if m.Artwork == nil {
		return
	}
	id := m.SourceID
	if id == 0 {
		id = m.TMDBID
	}
	prefix := fmt.Sprintf("/api/metadata/images/%s/%s/%d/", strings.ToLower(m.Source), m.Kind, id)
	if m.Artwork.ThumbPath != "" {
		m.ThumbURL = prefix + "thumb"
	}
	if m.Artwork.BackdropPath != "" {
		m.BackdropURL = prefix + "backdrop"
	}
}

type artworkCandidate struct {
	ref, language string
	text          bool
	score         float64
	width         int
}

func artworkLanguage(language string) int {
	switch strings.ToLower(language) {
	case "zh", "zho", "chi", "zh-cn", "zh-tw":
		return 3
	case "en", "eng":
		return 2
	case "", "null", "und":
		return 1
	default:
		return 0
	}
}

// Cover: localized/title-bearing landscape when available. Backdrop: clean,
// text-free landscape first. Stable provider order breaks exact quality ties.
func selectArtwork(candidates []artworkCandidate) TitleArtwork {
	pick := func(backdrop bool) string {
		best, priority := artworkCandidate{}, -1
		for _, c := range candidates {
			rank := artworkLanguage(c.language)
			if backdrop {
				if !c.text {
					rank += 8
				}
				if c.language == "" || c.language == "und" {
					rank += 4
				}
			} else if c.text {
				rank += 4
			}
			if rank > priority || (rank == priority && (c.score > best.score || (c.score == best.score && c.width > best.width))) {
				best, priority = c, rank
			}
		}
		return best.ref
	}
	return TitleArtwork{ThumbPath: pick(false), BackdropPath: pick(true)}
}

func validImage(data []byte, typ string) bool {
	return len(data) > 0 && (typ == "image/jpeg" || typ == "image/png" || typ == "image/webp") && http.DetectContentType(data) == typ
}

// Only stored, validated provider references are accepted. A caller can choose
// a type, not a URL. Cache reads stay independent of slow upstream downloads.
func (a *App) metadataImage(w http.ResponseWriter, r *http.Request) {
	source, kind, imageType := r.PathValue("source"), r.PathValue("kind"), r.PathValue("type")
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 || !validMetadataSource(source) || (kind != "movie" && kind != "series") || (imageType != "thumb" && imageType != "backdrop") {
		fail(w, 404, "图片类型或作品无效")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	var payload string
	var expires int64
	err = a.db.QueryRowContext(ctx, `SELECT payload,expires_at FROM metadata_cache WHERE source=? AND kind=? AND source_id=? AND expires_at>?`, source, kind, id, time.Now().Unix()).Scan(&payload, &expires)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		fail(w, 500, "无法读取图片资料")
		return
	}
	var m TitleMetadata
	if err != nil || json.Unmarshal([]byte(payload), &m) != nil || m.Artwork == nil {
		fail(w, 404, "图片不可用或资料已到期")
		return
	}
	ref := m.Artwork.ThumbPath
	if imageType == "backdrop" {
		ref = m.Artwork.BackdropPath
	}
	if ref == "" {
		fail(w, 404, "暂无此类图片")
		return
	}
	serveCached := func() bool {
		var data []byte
		var typ string
		var imageExpires, retry int64
		err := a.db.QueryRowContext(ctx, `SELECT image,content_type,expires_at,retry_at FROM metadata_images WHERE source=? AND kind=? AND source_id=? AND image_type=? AND image_path=?`, source, kind, id, imageType, ref).Scan(&data, &typ, &imageExpires, &retry)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			fail(w, 500, "无法读取图片缓存")
			return true
		}
		if imageExpires > time.Now().Unix() && len(data) > 0 {
			respondPhoto(w, r, data, typ)
			return true
		}
		if retry > time.Now().Unix() {
			fail(w, 404, "图片暂不可用，稍后重试")
			return true
		}
		return false
	}
	if serveCached() {
		return
	}
	select {
	case a.metadata.imageGate <- struct{}{}:
		defer func() { <-a.metadata.imageGate }()
	case <-ctx.Done():
		fail(w, 503, "图片加载超时")
		return
	}
	if serveCached() {
		return
	}
	data, typ, downloadErr := a.metadata.provider(source).image(ctx, ref, imageType)
	typ = strings.Split(typ, ";")[0]
	retry, imageExpires := int64(0), expires
	if downloadErr != nil || !validImage(data, typ) {
		data, typ, imageExpires, retry = nil, "", 0, time.Now().Add(24*time.Hour).Unix()
	}
	// Don't poison a cache on navigation cancellation. Guard the write against
	// expiry or a concurrent refresh replacing the selected artwork reference.
	if ctx.Err() != nil {
		fail(w, 503, "图片加载已取消或超时")
		return
	}
	result, err := a.db.ExecContext(ctx, `INSERT INTO metadata_images(source,kind,source_id,image_type,image_path,image,content_type,expires_at,retry_at)
 SELECT source,kind,source_id,?,?,?,?,?,? FROM metadata_cache WHERE source=? AND kind=? AND source_id=? AND expires_at>? AND payload=?
 ON CONFLICT(source,kind,source_id,image_type) DO UPDATE SET image_path=excluded.image_path,image=excluded.image,content_type=excluded.content_type,expires_at=excluded.expires_at,retry_at=excluded.retry_at`, imageType, ref, data, typ, imageExpires, retry, source, kind, id, time.Now().Unix(), payload)
	if err != nil {
		fail(w, 500, "无法缓存图片")
		return
	}
	if count, _ := result.RowsAffected(); count == 0 {
		fail(w, 404, "图片资料已变更或到期")
		return
	}
	if retry > 0 {
		fail(w, 404, "图片暂不可用，使用回退图")
		return
	}
	respondPhoto(w, r, data, typ)
}
