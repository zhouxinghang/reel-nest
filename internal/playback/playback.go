// Package playback owns checked media access, household progress, read-only
// stream credentials, bounded subtitles and irreversible single-file deletion.
// Authorization and HTTP representations belong to transport, never mediafs.
package playback

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"syscall"

	"reelnest/internal/mediafs"
	"reelnest/internal/model"
	"reelnest/internal/store"
)

var (
	ErrUnavailable    = errors.New("FILE_UNAVAILABLE")
	ErrChanged        = errors.New("FILE_CHANGED")
	ErrAccess         = errors.New("FILE_ACCESS_DENIED")
	ErrSubtitle       = errors.New("SUBTITLE_INVALID")
	ErrPreviewExpired = errors.New("DELETION_PREVIEW_EXPIRED")
	ErrTargetChanged  = errors.New("DELETION_TARGET_CHANGED")
)

// Files is the checked-handle boundary; implementations must never reopen an
// unchecked path. The production implementation is mediafs.FS. Tests may wrap
// actual file operations to exercise post-unlink cancellation or ambiguous I/O.
type Files interface {
	OpenVideo(mediafs.Ref) (*os.File, error)
	Sidecars(context.Context, mediafs.Ref, func([]mediafs.Sidecar) error) error
	OpenSubtitle(mediafs.Ref, mediafs.Sidecar) (*os.File, error)
	ObserveDeletion(mediafs.Ref) (mediafs.Observation, error)
	Unlink(context.Context, mediafs.Ref, mediafs.Observation) (mediafs.Outcome, error)
}

// Mutations owns the shared per-library scan/deletion lease and shutdown drain.
type Mutations interface {
	WithMutation(context.Context, string, func(context.Context) error) error
}

type Service struct {
	store     *store.Store
	fs        Files
	mutations Mutations
	key       [32]byte
}

func New(s *store.Store, fs Files, mutations Mutations) *Service {
	return &Service{store: s, fs: fs, mutations: mutations, key: s.Keys().Stream}
}

func reference(f model.MediaFile) mediafs.Ref {
	return mediafs.Ref{Root: mediafs.Root{Path: f.RootPath, Identity: f.RootIdentity}, Relative: f.Relative, Fingerprint: f.Fingerprint}
}

func fileError(e error) error {
	switch {
	case e == nil:
		return nil
	case errors.Is(e, context.Canceled), errors.Is(e, context.DeadlineExceeded):
		return e
	case errors.Is(e, os.ErrNotExist):
		return ErrUnavailable
	case errors.Is(e, mediafs.ErrChanged), errors.Is(e, mediafs.ErrPath), errors.Is(e, syscall.ELOOP), errors.Is(e, syscall.ENOTDIR):
		return ErrChanged
	default:
		// Do not return OS errors containing absolute paths or mount details.
		return ErrAccess
	}
}

func (s *Service) media(ctx context.Context, id string) (model.MediaFile, error) {
	f, e := s.store.MediaFile(ctx, id)
	if e == nil && f.State != "available" {
		e = ErrUnavailable
	}
	return f, e
}

// Open returns the very handle checked against the indexed file version. The
// caller closes it; Range/HEAD can seek this handle without whole-file buffering.
func (s *Service) Open(ctx context.Context, id string) (*os.File, error) {
	f, e := s.media(ctx, id)
	if e != nil {
		return nil, e
	}
	file, e := s.fs.OpenVideo(reference(f))
	return file, fileError(e)
}

func (s *Service) Preflight(ctx context.Context, id string) (*model.Progress, error) {
	f, e := s.Open(ctx, id)
	if e != nil {
		return nil, e
	}
	f.Close()
	return s.Progress(ctx, id)
}

func (s *Service) Progress(ctx context.Context, id string) (*model.Progress, error) {
	return s.store.Progress(ctx, id)
}

func (s *Service) SaveProgress(ctx context.Context, id string, p model.Progress) (model.Progress, error) {
	// No file mutation lease or upstream work. Store validates finite values and
	// target state in the same short transaction as the last-received update.
	p, e := s.store.SaveProgress(ctx, id, p)
	if errors.Is(e, model.ErrConflict) {
		e = ErrUnavailable
	}
	return p, e
}

func (s *Service) sign(domain string, payload []byte) []byte {
	m := hmac.New(sha256.New, s.key[:])
	m.Write([]byte("reelnest:" + domain + ":v1\x00"))
	m.Write(payload)
	return m.Sum(nil)
}

func (s *Service) StreamLink(ctx context.Context, id string) (string, error) {
	f, e := s.Open(ctx, id)
	if e != nil {
		return "", e
	}
	f.Close()
	// Origin-independent; never interpolate Host or forwarding headers.
	return "/stream/" + id + "?token=" + hex.EncodeToString(s.sign("read-media", []byte(id))), nil
}

func (s *Service) ValidStreamToken(id, token string) bool {
	if len(token) != 64 {
		return false
	}
	b, e := hex.DecodeString(token)
	return e == nil && hmac.Equal(b, s.sign("read-media", []byte(id)))
}
