package http

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"reelnest/internal/model"
	"reelnest/internal/playback"
)

func progress(p *model.Progress) *progressDTO {
	if p == nil {
		return nil
	}
	return &progressDTO{p.Position, p.Duration, p.Completed, p.UpdatedAt}
}

type playbackDTO struct {
	MediaID     string       `json:"mediaId"`
	ContentPath string       `json:"contentPath"`
	Progress    *progressDTO `json:"progress"`
}
type savedProgressDTO struct {
	Progress *progressDTO `json:"progress"`
}
type streamLinkDTO struct {
	Path string `json:"path"`
}

func (t *Transport) playbackStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, e := t.playback.Preflight(r.Context(), id)
	if e != nil {
		t.fail(w, e)
		return
	}
	respond(w, 200, playbackDTO{id, "/api/media/" + id + "/content", progress(p)})
}
func (t *Transport) mediaProgress(w http.ResponseWriter, r *http.Request) {
	p, e := t.playback.Progress(r.Context(), r.PathValue("id"))
	if e != nil {
		t.fail(w, e)
		return
	}
	respond(w, 200, savedProgressDTO{progress(p)})
}
func (t *Transport) saveMediaProgress(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Position  float64 `json:"position"`
		Duration  float64 `json:"duration"`
		Completed bool    `json:"completed"`
	}
	if json.NewDecoder(r.Body).Decode(&input) != nil {
		t.fail(w, model.ErrInvalid)
		return
	}
	p, e := t.playback.SaveProgress(r.Context(), r.PathValue("id"), model.Progress{Position: input.Position, Duration: input.Duration, Completed: input.Completed})
	if e != nil {
		t.fail(w, e)
		return
	}
	respond(w, 200, progress(&p))
}

var mediaTypes = map[string]string{".mp4": "video/mp4", ".m4v": "video/mp4", ".webm": "video/webm", ".mov": "video/quicktime", ".mkv": "video/x-matroska", ".avi": "video/x-msvideo", ".ts": "video/mp2t", ".m2ts": "video/mp2t", ".mpg": "video/mpeg", ".mpeg": "video/mpeg", ".wmv": "video/x-ms-wmv", ".flv": "video/x-flv"}

func (t *Transport) mediaContent(w http.ResponseWriter, r *http.Request) {
	f, e := t.playback.Open(r.Context(), r.PathValue("id"))
	if e != nil {
		t.fail(w, e)
		return
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		t.fail(w, playback.ErrAccess)
		return
	}
	w.Header().Set("Content-Type", mediaTypes[strings.ToLower(filepath.Ext(info.Name()))])
	w.Header().Set("ETag", `"`+r.PathValue("id")+`"`)
	// Immutable IDs identify file versions; ServeContent seeks the checked handle.
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}
func (t *Transport) streamLink(w http.ResponseWriter, r *http.Request) {
	path, e := t.playback.StreamLink(r.Context(), r.PathValue("id"))
	if e != nil {
		t.fail(w, e)
		return
	}
	respond(w, 200, streamLinkDTO{path})
}

type subtitleDTO struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	Format   string `json:"format"`
}
type subtitlesDTO struct {
	Items []subtitleDTO `json:"items"`
	Page  pageInfo      `json:"page"`
}

func (t *Transport) mediaSubtitles(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, _ = strconv.Atoi(raw)
	}
	p, e := t.playback.Subtitles(r.Context(), r.PathValue("id"), r.URL.Query().Get("cursor"), limit)
	if e != nil {
		t.fail(w, e)
		return
	}
	out := subtitlesDTO{Items: []subtitleDTO{}, Page: pageInfo{p.NextCursor, p.ExpiresAt}}
	for _, sub := range p.Items {
		out.Items = append(out.Items, subtitleDTO{sub.ID, sub.Filename, sub.Format})
	}
	respond(w, 200, out)
}
func (t *Transport) subtitleContent(w http.ResponseWriter, r *http.Request) {
	vtt, e := t.playback.Subtitle(r.Context(), r.PathValue("id"), r.PathValue("subtitle"))
	if e != nil {
		t.fail(w, e)
		return
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	_, _ = io.WriteString(w, vtt)
}

type deletionPreviewDTO struct {
	MediaID      string `json:"mediaId"`
	Path         string `json:"path"`
	Confirmation string `json:"confirmation"`
	ExpiresAt    int64  `json:"expiresAt"`
	Warning      string `json:"warning"`
}
type deleteDTO struct {
	FileOutcome  string `json:"fileOutcome"`
	IndexOutcome string `json:"indexOutcome"`
	Message      string `json:"message"`
	Retryable    bool   `json:"retryable"`
	RequestID    string `json:"requestId"`
}

func (t *Transport) deletionPreview(w http.ResponseWriter, r *http.Request) {
	p, e := t.playback.DeletionPreview(r.Context(), r.PathValue("id"))
	if e != nil {
		t.fail(w, e)
		return
	}
	respond(w, 200, deletionPreviewDTO{p.MediaID, p.Path, p.Confirmation, p.ExpiresAt, p.Warning})
}
func (t *Transport) deleteMedia(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Confirmation string `json:"confirmation"`
		Confirm      bool   `json:"confirm"`
	}
	if json.NewDecoder(r.Body).Decode(&input) != nil {
		t.fail(w, model.ErrInvalid)
		return
	}
	out, e := t.playback.Delete(r.Context(), r.PathValue("id"), input.Confirmation, input.Confirm)
	if e != nil {
		t.failWithRetry(w, e, false)
		return
	}
	respond(w, 200, deleteDTO{string(out.FileOutcome), out.IndexOutcome, out.Message, false, model.NewID()})
}
