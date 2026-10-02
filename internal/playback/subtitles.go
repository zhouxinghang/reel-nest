package playback

import (
	"container/heap"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"reelnest/internal/mediafs"
	"reelnest/internal/model"
)

type Subtitle struct{ ID, Filename, Format string }
type SubtitlePage struct {
	Items      []Subtitle
	NextCursor *string
	ExpiresAt  *int64
}
type subtitleCursor struct {
	MediaID, After, Revision string
	Limit                    int
	Expires                  int64
}

func subtitleID(id string, sidecar mediafs.Sidecar) string {
	h := sha256.Sum256([]byte("reelnest:subtitle:v1\x00" + id + "\x00" + sidecar.Name + "\x00" + sidecar.Fingerprint))
	return hex.EncodeToString(h[:])
}

// Keep only the lexically smallest limit+1 candidates after the cursor. A
// maximum heap bounds retained filenames even for an unusually large directory.
type subtitleHeap []Subtitle

func (h subtitleHeap) Len() int           { return len(h) }
func (h subtitleHeap) Less(i, j int) bool { return h[i].Filename > h[j].Filename }
func (h subtitleHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *subtitleHeap) Push(v any)        { *h = append(*h, v.(Subtitle)) }
func (h *subtitleHeap) Pop() any          { a := *h; v := a[len(a)-1]; *h = a[:len(a)-1]; return v }

func (s *Service) decodeSubtitleCursor(raw, id string, limit int) (subtitleCursor, error) {
	c := subtitleCursor{MediaID: id, Limit: limit, Expires: time.Now().Add(5 * time.Minute).UnixMilli()}
	if raw == "" {
		return c, nil
	}
	if len(raw) > 4096 {
		return c, model.ErrCursor
	}
	p, sig, ok := strings.Cut(raw, ".")
	b, e := base64.RawURLEncoding.DecodeString(sig)
	if !ok || e != nil || !hmac.Equal(b, s.sign("subtitle-page", []byte(p))) {
		return c, model.ErrCursor
	}
	b, e = base64.RawURLEncoding.DecodeString(p)
	if e != nil || json.Unmarshal(b, &c) != nil || c.MediaID != id || c.Limit != limit || c.Expires <= time.Now().UnixMilli() {
		return c, model.ErrCursor
	}
	return c, nil
}

func (s *Service) Subtitles(ctx context.Context, id, rawCursor string, limit int) (SubtitlePage, error) {
	if limit < 1 || limit > 100 {
		return SubtitlePage{}, model.ErrInvalid
	}
	c, e := s.decodeSubtitleCursor(rawCursor, id, limit)
	if e != nil {
		return SubtitlePage{}, e
	}
	f, e := s.media(ctx, id)
	if e != nil {
		return SubtitlePage{}, e
	}
	h := &subtitleHeap{}
	// Order-independent digest: directory enumeration order is not stable. Each
	// unique name/version contributes a SHA-256 value, without storing the list.
	var revision [32]byte
	e = s.fs.Sidecars(ctx, reference(f), func(batch []mediafs.Sidecar) error {
		for _, sub := range batch {
			subID := subtitleID(id, sub)
			b, _ := hex.DecodeString(subID)
			for n := range revision {
				revision[n] ^= b[n]
			}
			if sub.Name <= c.After {
				continue
			}
			heap.Push(h, Subtitle{subID, sub.Name, sub.Format})
			if h.Len() > limit+1 {
				heap.Pop(h)
			}
		}
		return ctx.Err()
	})
	if e != nil {
		return SubtitlePage{}, fileError(e)
	}
	rev := hex.EncodeToString(revision[:])
	if rawCursor != "" && c.Revision != rev {
		return SubtitlePage{}, model.ErrCursor
	}
	items := []Subtitle(*h)
	sort.Slice(items, func(i, j int) bool { return items[i].Filename < items[j].Filename })
	p := SubtitlePage{Items: items}
	if len(items) > limit {
		p.Items = items[:limit]
		c.After, c.Revision = p.Items[len(p.Items)-1].Filename, rev
		b, _ := json.Marshal(c)
		payload := base64.RawURLEncoding.EncodeToString(b)
		cursor := payload + "." + base64.RawURLEncoding.EncodeToString(s.sign("subtitle-page", []byte(payload)))
		p.NextCursor, p.ExpiresAt = &cursor, &c.Expires
	}
	return p, nil
}

func (s *Service) Subtitle(ctx context.Context, id, subtitle string) (string, error) {
	f, e := s.media(ctx, id)
	if e != nil {
		return "", e
	}
	ref := reference(f)
	var selected *mediafs.Sidecar
	e = s.fs.Sidecars(ctx, ref, func(batch []mediafs.Sidecar) error {
		for _, sub := range batch {
			if subtitleID(id, sub) == subtitle {
				copy := sub
				selected = &copy
			}
		}
		return ctx.Err()
	})
	if e != nil {
		return "", fileError(e)
	}
	if selected == nil {
		return "", model.ErrNotFound
	}
	file, e := s.fs.OpenSubtitle(ref, *selected)
	if e != nil {
		return "", fileError(e)
	}
	defer file.Close()
	before, e := file.Stat()
	if e != nil {
		return "", fileError(e)
	}
	if before.Size() > SubtitleLimit {
		return "", ErrSubtitle
	}
	if e = ctx.Err(); e != nil {
		return "", e
	}
	b, e := io.ReadAll(io.LimitReader(file, SubtitleLimit+1))
	if e != nil {
		return "", fileError(e)
	}
	after, e := file.Stat()
	if e != nil {
		return "", fileError(e)
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return "", ErrChanged
	}
	if e = ctx.Err(); e != nil {
		return "", e
	}
	vtt, e := SubtitleVTT(b, selected.Format)
	if e != nil {
		return "", errors.Join(ErrSubtitle, e)
	}
	return vtt, nil
}
