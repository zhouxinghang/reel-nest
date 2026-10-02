package playback

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"reelnest/internal/catalog"
	"reelnest/internal/mediafs"
	"reelnest/internal/model"
	"reelnest/internal/store"
)

type fixture struct {
	s                *store.Store
	c                *catalog.Catalog
	fs               *mediafs.FS
	p                *Service
	f                model.MediaFile
	path, data, root string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	data, root := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "one.webm")
	if e := os.WriteFile(path, []byte("isolated video"), 0600); e != nil {
		t.Fatal(e)
	}
	s, e := store.Open(ctx, data)
	if e != nil {
		t.Fatal(e)
	}
	fs, e := mediafs.New([]string{root}, data)
	if e != nil {
		t.Fatal(e)
	}
	c, e := catalog.New(ctx, s, fs)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := c.Close(); e != nil {
			t.Error(e)
		}
		if e := s.Close(); e != nil {
			t.Error(e)
		}
	})
	lib, e := c.Create(ctx, "测试", root, "movie", []string{})
	if e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		scan, e := s.Scan(ctx, *lib.LatestScanID)
		if e != nil {
			t.Fatal(e)
		}
		if scan.State == "completed" && c.WithMutation(ctx, lib.ID, func(context.Context) error { return nil }) == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("scan did not release", scan)
		}
		time.Sleep(time.Millisecond)
	}
	files, e := s.MediaFiles(ctx, lib.ID, "", 10)
	if e != nil || len(files) != 1 {
		t.Fatal(files, e)
	}
	return &fixture{s, c, fs, New(s, fs, c), files[0], path, data, root}
}
func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, e := os.Stat(path); e != nil {
		t.Fatal(e)
	}
}
func mustAbsent(t *testing.T, path string) {
	t.Helper()
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("expected missing", e)
	}
}

type unlinkWrapper struct {
	Files
	run func(context.Context, mediafs.Ref, mediafs.Observation) (mediafs.Outcome, error)
}

func (f unlinkWrapper) Unlink(ctx context.Context, ref mediafs.Ref, o mediafs.Observation) (mediafs.Outcome, error) {
	return f.run(ctx, ref, o)
}

func TestDeletionCancelledBeforeUnlinkAndExpiredAtFinalCheck(t *testing.T) {
	a := setup(t)
	ctx := context.Background()
	p, e := a.p.DeletionPreview(ctx, a.f.ID)
	if e != nil {
		t.Fatal(e)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, e = a.p.Delete(cancelled, a.f.ID, p.Confirmation, true); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	mustExist(t, a.path)
	observed, e := a.fs.ObserveDeletion(reference(a.f))
	if e != nil {
		t.Fatal(e)
	}
	// Wrap only the real file operation to pause until the signed expiry deadline.
	a.p.fs = unlinkWrapper{a.fs, func(ctx context.Context, ref mediafs.Ref, o mediafs.Observation) (mediafs.Outcome, error) {
		<-ctx.Done()
		return a.fs.Unlink(ctx, ref, o)
	}}
	token := a.p.confirmation(a.f.ID, observed, time.Now().Add(30*time.Millisecond).UnixMilli())
	if _, e = a.p.Delete(ctx, a.f.ID, token, true); !errors.Is(e, ErrPreviewExpired) {
		t.Fatal(e)
	}
	mustExist(t, a.path)
	if _, e = a.p.Delete(ctx, a.f.ID, p.Confirmation, false); !errors.Is(e, ErrTargetChanged) {
		t.Fatal(e)
	}
}
func TestDisconnectAfterActualUnlinkStillUpdatesIndex(t *testing.T) {
	a := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p, e := a.p.DeletionPreview(ctx, a.f.ID)
	if e != nil {
		t.Fatal(e)
	}
	a.p.fs = unlinkWrapper{a.fs, func(ctx context.Context, ref mediafs.Ref, o mediafs.Observation) (mediafs.Outcome, error) {
		out, e := a.fs.Unlink(ctx, ref, o)
		cancel()
		return out, e
	}}
	out, e := a.p.Delete(ctx, a.f.ID, p.Confirmation, true)
	if e != nil || out.FileOutcome != mediafs.Deleted || out.IndexOutcome != "updated" {
		t.Fatal(out, e)
	}
	mustAbsent(t, a.path)
	f, e := a.s.MediaFile(context.Background(), a.f.ID)
	if e != nil || f.State != "deleted" {
		t.Fatal(f, e)
	}
}
func TestUnknownUnlinkOutcomeNeverClaimsDeletionOrRetries(t *testing.T) {
	for _, didDelete := range []bool{false, true} {
		t.Run(map[bool]string{false: "file remains", true: "file removed"}[didDelete], func(t *testing.T) {
			a := setup(t)
			ctx := context.Background()
			p, e := a.p.DeletionPreview(ctx, a.f.ID)
			if e != nil {
				t.Fatal(e)
			}
			calls := 0
			a.p.fs = unlinkWrapper{a.fs, func(ctx context.Context, ref mediafs.Ref, o mediafs.Observation) (mediafs.Outcome, error) {
				calls++
				if didDelete {
					out, e := a.fs.Unlink(ctx, ref, o)
					if e != nil || out != mediafs.Deleted {
						t.Fatal(out, e)
					}
				}
				return mediafs.Unknown, unix.EIO
			}}
			out, e := a.p.Delete(ctx, a.f.ID, p.Confirmation, true)
			if e != nil || out.FileOutcome != mediafs.Unknown || out.IndexOutcome != "pending" || calls != 1 {
				t.Fatal(out, e, calls)
			}
			if didDelete {
				mustAbsent(t, a.path)
			} else {
				mustExist(t, a.path)
			}
			f, e := a.s.MediaFile(ctx, a.f.ID)
			if e != nil || f.State != "available" {
				t.Fatal("ambiguous result became certain", f, e)
			}
		})
	}
}
func TestShutdownDrainsPostUnlinkAndRejectsNewDeletion(t *testing.T) {
	a := setup(t)
	ctx := context.Background()
	p, e := a.p.DeletionPreview(ctx, a.f.ID)
	if e != nil {
		t.Fatal(e)
	}
	unlinked, release := make(chan struct{}), make(chan struct{})
	// Cleanup always releases the operation before catalog/store cleanup can wait.
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	a.p.fs = unlinkWrapper{a.fs, func(ctx context.Context, ref mediafs.Ref, o mediafs.Observation) (mediafs.Outcome, error) {
		out, e := a.fs.Unlink(ctx, ref, o)
		close(unlinked)
		<-ctx.Done()
		<-release
		return out, e
	}}
	type result struct {
		out DeleteResult
		e   error
	}
	finished := make(chan result, 1)
	go func() { out, e := a.p.Delete(ctx, a.f.ID, p.Confirmation, true); finished <- result{out, e} }()
	select {
	case <-unlinked:
	case <-time.After(5 * time.Second):
		t.Fatal("unlink timeout")
	}
	closed := make(chan error, 1)
	go func() { closed <- a.c.Close() }()
	deadline := time.Now().Add(time.Second)
	for a.c.Ready() {
		if time.Now().After(deadline) {
			t.Fatal("shutdown timeout")
		}
		time.Sleep(time.Millisecond)
	}
	if _, e = a.p.DeletionPreview(ctx, a.f.ID); !errors.Is(e, model.ErrClosed) {
		t.Fatal("accepted after shutdown", e)
	}
	select {
	case e := <-closed:
		t.Fatal("closed during reconciliation", e)
	default:
	}
	close(release)
	released = true
	select {
	case result := <-finished:
		if result.e != nil || result.out.IndexOutcome != "updated" {
			t.Fatal(result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("delete timeout")
	}
	if e := <-closed; e != nil {
		t.Fatal(e)
	}
	f, e := a.s.MediaFile(ctx, a.f.ID)
	if e != nil || f.State != "deleted" {
		t.Fatal(f, e)
	}
}
func TestProgressValidationAndStableReadOnlyCredentialsAcrossRestart(t *testing.T) {
	a := setup(t)
	ctx := context.Background()
	for _, p := range []model.Progress{{Position: math.NaN(), Duration: 60}, {Position: 1, Duration: math.Inf(1)}, {Position: 61, Duration: 60}, {Position: 5, Duration: 60, Completed: true}} {
		if _, e := a.p.SaveProgress(ctx, a.f.ID, p); !errors.Is(e, model.ErrInvalid) {
			t.Fatal(p, e)
		}
	}
	before, e := a.p.SaveProgress(ctx, a.f.ID, model.Progress{Position: 12.5, Duration: 60})
	if e != nil {
		t.Fatal(e)
	}
	link, e := a.p.StreamLink(ctx, a.f.ID)
	if e != nil {
		t.Fatal(e)
	}
	token := strings.Split(link, "?token=")[1]
	if !a.p.ValidStreamToken(a.f.ID, token) || a.p.ValidStreamToken("other", token) || a.p.ValidStreamToken(a.f.ID, token+"x") {
		t.Fatal("invalid scope")
	}
	if e = a.c.Close(); e != nil {
		t.Fatal(e)
	}
	if e = a.s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e := store.Open(ctx, a.data)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	c, e := catalog.New(ctx, s, a.fs)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	p := New(s, a.fs, c)
	next, e := p.StreamLink(ctx, a.f.ID)
	if e != nil || next != link {
		t.Fatal("rotated credential", next, e)
	}
	saved, e := p.Preflight(ctx, a.f.ID)
	if e != nil || saved == nil || *saved != before {
		t.Fatal(saved, e)
	}
	observed, e := a.fs.ObserveDeletion(reference(a.f))
	if e != nil {
		t.Fatal(e)
	}
	// A stream MAC is not a deletion confirmation (different signed domain).
	bad := strings.Split(p.confirmation(a.f.ID, observed, time.Now().Add(time.Minute).UnixMilli()), ".")[0] + "." + token
	if _, e = p.Delete(ctx, a.f.ID, bad, true); !errors.Is(e, ErrTargetChanged) {
		t.Fatal(e)
	}
	mustExist(t, a.path)
}
func TestSubtitleCursorExpiryAndPureValidation(t *testing.T) {
	a := setup(t)
	for _, input := range []struct{ data, format string }{
		{"WEBVTT\n\n00:00.000 --> 00:01.000\nhello", "ass"},
		{"WEBVTT\n\n00:00.000 --> 00:01.000 align:left align:right\nhello", "vtt"},
		{"WEBVTT\n\n00:00.000 --> 00:01.000 position:100%,start\nhello", "vtt"},
		{"WEBVTT\n\n00:00.000 --> 00:01.000 size:-1%\nhello", "vtt"},
		{"1\n999999:00:00,000 --> 999999:00:01,000\nhello", "srt"},
	} {
		if _, e := SubtitleVTT([]byte(input.data), input.format); e == nil {
			t.Fatal(input)
		}
	}
	for _, settings := range []string{"line:-1,start position:25%,line-left size:50% align:left", "vertical:rl line:auto position:30%,auto", "line:50%,end align:end"} {
		if _, e := SubtitleVTT([]byte("WEBVTT\n\n00:00.000 --> 00:01.000 "+settings+"\n你好"), "vtt"); e != nil {
			t.Fatal(settings, e)
		}
	}
	// Use the actual signer, then an already-expired signed cursor, not a bad MAC.
	c := subtitleCursor{MediaID: a.f.ID, Limit: 10, Expires: time.Now().Add(-time.Second).UnixMilli()}
	raw := encodeExpiredCursor(a.p, c)
	if _, e := a.p.Subtitles(context.Background(), a.f.ID, raw, 10); !errors.Is(e, model.ErrCursor) {
		t.Fatal(e)
	}
}

func encodeExpiredCursor(s *Service, c subtitleCursor) string {
	b, _ := json.Marshal(c)
	payload := base64.RawURLEncoding.EncodeToString(b)
	return payload + "." + base64.RawURLEncoding.EncodeToString(s.sign("subtitle-page", []byte(payload)))
}
