package playback

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"reelnest/internal/mediafs"
	"reelnest/internal/model"
)

const PreviewLifetime = 10 * time.Minute
const ReconcileTimeout = 10 * time.Second
const deletionWarning = "源文件将永久删除，本系统无法恢复。仅删除此视频，不删除字幕、海报、其他视频或目录。"

type Preview struct {
	MediaID, Path, Confirmation, Warning string
	ExpiresAt                            int64
}

type DeleteResult struct {
	FileOutcome           mediafs.Outcome
	IndexOutcome, Message string
}

func (s *Service) confirmation(id string, observed mediafs.Observation, expires int64) string {
	payload, _ := json.Marshal(struct {
		ID       string
		Observed mediafs.Observation
		Expires  int64
	}{id, observed, expires})
	return strconv.FormatInt(expires, 10) + "." + hex.EncodeToString(s.sign("delete-confirmation", payload))
}

func (s *Service) DeletionPreview(ctx context.Context, id string) (Preview, error) {
	f, e := s.media(ctx, id)
	if e != nil {
		return Preview{}, e
	}
	var p Preview
	e = s.mutations.WithMutation(ctx, f.LibraryID, func(ctx context.Context) error {
		// Reload after acquiring the shared scan/deletion lease.
		current, e := s.media(ctx, id)
		if e != nil {
			return e
		}
		observed, e := s.fs.ObserveDeletion(reference(current))
		if e != nil {
			return fileError(e)
		}
		expires := time.Now().Add(PreviewLifetime).UnixMilli()
		p = Preview{id, observed.Path, s.confirmation(id, observed, expires), deletionWarning, expires}
		return ctx.Err()
	})
	return p, e
}

// Delete returns an ordinary error ONLY before unlink could have happened.
// Possible/confirmed unlink always returns a structured, non-retryable outcome.
// Both endpoints still require household authorization in transport.
func (s *Service) Delete(ctx context.Context, id, confirmation string, confirm bool) (DeleteResult, error) {
	expiry, signature, ok := strings.Cut(confirmation, ".")
	expires, e := strconv.ParseInt(expiry, 10, 64)
	if !confirm || !ok || e != nil || len(signature) != 64 {
		return DeleteResult{}, ErrTargetChanged
	}
	now := time.Now()
	if expires <= now.UnixMilli() || expires > now.Add(PreviewLifetime).UnixMilli() {
		return DeleteResult{}, ErrPreviewExpired
	}
	f, e := s.media(ctx, id)
	if e != nil {
		return DeleteResult{}, e
	}
	var result DeleteResult
	e = s.mutations.WithMutation(ctx, f.LibraryID, func(ctx context.Context) error {
		current, e := s.media(ctx, id)
		if e != nil {
			return e
		}
		ref := reference(current)
		observed, e := s.fs.ObserveDeletion(ref)
		if e != nil {
			return fileError(e)
		}
		if !hmac.Equal([]byte(confirmation), []byte(s.confirmation(id, observed, expires))) {
			return ErrTargetChanged
		}
		// mediafs checks cancellation immediately before unlink. Enforce expiry at
		// that same boundary, not only before potentially slow filesystem checks.
		unlinkCtx, cancel := context.WithDeadline(ctx, time.UnixMilli(expires))
		outcome, unlinkErr := s.fs.Unlink(unlinkCtx, ref, observed)
		cancel()
		if outcome == mediafs.NotDeleted {
			if time.Now().UnixMilli() >= expires {
				return ErrPreviewExpired
			}
			if errors.Is(unlinkErr, mediafs.ErrChanged) {
				return ErrTargetChanged
			}
			if unlinkErr != nil {
				return fileError(unlinkErr)
			}
			return model.ErrInvalid
		}
		result = DeleteResult{FileOutcome: mediafs.Unknown, IndexOutcome: "pending", Message: "删除结果未知，索引未更新。请核查媒体目录并重新扫描；不要直接重试删除。"}
		if outcome == mediafs.Deleted {
			result.FileOutcome = mediafs.Deleted
			result.Message = "源文件已永久删除，但索引待核对。本系统无法恢复；请修复应用数据存储后重新扫描，不要重复删除。"
			// A disconnected client or shutdown must not skip reconciliation. The
			// mutation lease stays held until this bounded independent work ends.
			reconcileCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), ReconcileTimeout)
			e = s.store.MarkFileDeleted(reconcileCtx, id)
			stop()
			if e == nil {
				result.IndexOutcome = "updated"
				result.Message = "源文件已永久删除，本系统无法恢复。字幕、海报、其他视频及目录均未删除。"
			}
		}
		// Neither raw errors nor paths/confirmation tokens are logged.
		slog.Info("media deletion outcome", "media_id", id, "file_outcome", result.FileOutcome, "index_outcome", result.IndexOutcome)
		return nil
	})
	return result, e
}
