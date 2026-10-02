package catalog_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"reelnest/internal/catalog"
	"reelnest/internal/identity"
	"reelnest/internal/mediafs"
	"reelnest/internal/model"
	"reelnest/internal/store"
)

type instance struct {
	s          *store.Store
	c          *catalog.Catalog
	fs         *mediafs.FS
	root, data string
}

func setup(t *testing.T) *instance { t.Helper(); return open(t, t.TempDir(), t.TempDir()) }
func open(t *testing.T, data, root string) *instance {
	t.Helper()
	s, e := store.Open(context.Background(), data)
	if e != nil {
		t.Fatal(e)
	}
	fs, e := mediafs.New([]string{root}, data)
	if e != nil {
		t.Fatal(e)
	}
	c, e := catalog.New(context.Background(), s, fs)
	if e != nil {
		t.Fatal(e)
	}
	a := &instance{s, c, fs, root, data}
	t.Cleanup(func() {
		if e := c.Close(); e != nil {
			t.Error(e)
		}
		if e := s.Close(); e != nil {
			t.Error(e)
		}
	})
	return a
}
func write(t *testing.T, path string) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, []byte("isolated video"), 0600); e != nil {
		t.Fatal(e)
	}
}
func waitScan(t *testing.T, s *store.Store, id string, predicate func(model.Scan) bool) model.Scan {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		scan, e := s.Scan(context.Background(), id)
		if e != nil {
			t.Fatal(e)
		}
		if predicate(scan) {
			return scan
		}
		if scan.State != "queued" && scan.State != "running" {
			t.Fatalf("unexpected scan terminal state: %+v", scan)
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("scan timeout")
	return model.Scan{}
}
func complete(t *testing.T, s *store.Store, id string) model.Scan {
	return waitScan(t, s, id, func(scan model.Scan) bool { return scan.State == "completed" })
}
func (a *instance) create(t *testing.T, relative string) model.Library {
	t.Helper()
	path := filepath.Join(a.root, relative)
	if e := os.MkdirAll(path, 0700); e != nil {
		t.Fatal(e)
	}
	l, e := a.c.Create(context.Background(), "库", path, "series", []string{})
	if e != nil {
		t.Fatal(e)
	}
	complete(t, a.s, *l.LatestScanID)
	deadline := time.Now().Add(time.Second)
	for {
		e := a.c.WithMutation(context.Background(), l.ID, func(context.Context) error { return nil })
		if e == nil {
			break
		}
		if !errors.Is(e, model.ErrBusy) || time.Now().After(deadline) {
			t.Fatal(e)
		}
		time.Sleep(time.Millisecond)
	}
	return l
}
func startScan(t *testing.T, c *catalog.Catalog, id string) model.Scan {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		scan, e := c.StartScan(context.Background(), id)
		if e == nil {
			return scan
		}
		if !errors.Is(e, model.ErrBusy) || time.Now().After(deadline) {
			t.Fatal(e)
		}
		time.Sleep(time.Millisecond)
	}
}
func TestPerLibraryMutationDoesNotBlockReadsProgressOrOtherLibraries(t *testing.T) {
	a := setup(t)
	write(t, filepath.Join(a.root, "first", "Show/01.mp4"))
	l := a.create(t, "first")
	f, e := a.s.MediaFiles(context.Background(), l.ID, "", 10)
	if e != nil || len(f) != 1 {
		t.Fatal(f, e)
	}
	held, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- a.c.WithMutation(context.Background(), l.ID, func(ctx context.Context) error { close(held); <-release; return nil })
	}()
	<-held
	if _, e := a.c.StartScan(context.Background(), l.ID); !errors.Is(e, model.ErrBusy) {
		t.Fatal("same library was not mutually exclusive", e)
	}
	if e := a.c.WithMutation(context.Background(), l.ID, func(context.Context) error { return nil }); !errors.Is(e, model.ErrBusy) {
		t.Fatal(e)
	}
	other := a.create(t, "other")
	if other.ID == l.ID {
		t.Fatal("same-name libraries merged")
	}
	video, e := a.c.OpenVideo(context.Background(), f[0].ID)
	if e != nil {
		t.Fatal(e)
	}
	video.Close()
	if _, e := a.s.SaveProgress(context.Background(), f[0].ID, model.Progress{Position: 1, Duration: 60}); e != nil {
		t.Fatal(e)
	}
	close(release)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
}
func TestBoundedWorkersCrossLibraryScanningAndCancelledPublication(t *testing.T) {
	if testing.Short() {
		t.Skip("real large-directory traversal")
	}
	a := setup(t)
	write(t, filepath.Join(a.root, "large", "Show/01.mp4"))
	l := a.create(t, "large")
	other := a.create(t, "small")
	before, e := a.s.MediaFiles(context.Background(), l.ID, "", 10)
	if e != nil {
		t.Fatal(e)
	}
	// Enough real entries to observe staging; no mocked scanner or sleep in I/O.
	for n := 0; n < 12000; n++ {
		write(t, filepath.Join(a.root, "large", fmt.Sprintf("Show/%05d.mp4", n+2)))
	}
	scan := startScan(t, a.c, l.ID)
	waitScan(t, a.s, scan.ID, func(s model.Scan) bool { return s.State == "running" && s.DiscoveredCount >= 256 })
	if _, e = a.c.StartScan(context.Background(), l.ID); !errors.Is(e, model.ErrBusy) {
		t.Fatal(e)
	}
	second := startScan(t, a.c, other.ID)
	small := complete(t, a.s, second.ID)
	// The other worker can fully publish the small library while the first walks.
	current, e := a.s.Scan(context.Background(), scan.ID)
	if e != nil || current.State != "running" {
		t.Fatalf("cross-library traversal serialized: %+v %v", current, e)
	}
	if current.StartedAt == nil || small.StartedAt == nil || *small.StartedAt < *current.StartedAt {
		t.Fatal(current, small)
	}
	read, e := a.c.OpenVideo(context.Background(), before[0].ID)
	if e != nil {
		t.Fatal(e)
	}
	read.Close()
	if _, e = a.s.SaveProgress(context.Background(), before[0].ID, model.Progress{Position: 2, Duration: 60}); e != nil {
		t.Fatal(e)
	}
	if e = a.c.Close(); e != nil {
		t.Fatal(e)
	}
	current, e = a.s.Scan(context.Background(), scan.ID)
	if e != nil || current.State != "interrupted" {
		t.Fatal(current, e)
	}
	after, e := a.s.MediaFiles(context.Background(), l.ID, "", 256)
	if e != nil || len(after) != 1 || after[0].ID != before[0].ID {
		t.Fatal("partial cancelled snapshot published", len(after), e)
	}
	if _, e = a.c.StartScan(context.Background(), l.ID); !errors.Is(e, model.ErrClosed) {
		t.Fatal(e)
	}
}
func TestCloseWaitsForPostUnlinkReconciliationAndRejectsNewWork(t *testing.T) {
	a := setup(t)
	write(t, filepath.Join(a.root, "tv", "Show/01.mp4"))
	l := a.create(t, "tv")
	files, e := a.s.MediaFiles(context.Background(), l.ID, "", 10)
	if e != nil {
		t.Fatal(e)
	}
	f := files[0]
	removed, finish := make(chan struct{}), make(chan struct{})
	operation := make(chan error, 1)
	go func() {
		operation <- a.c.WithMutation(context.Background(), l.ID, func(ctx context.Context) error {
			ref := catalog.Reference(f)
			observed, e := a.fs.ObserveDeletion(ref)
			if e != nil {
				return e
			}
			result, e := a.fs.Unlink(ctx, ref, observed)
			if e != nil || result != mediafs.Deleted {
				return fmt.Errorf("unlink %s: %w", result, e)
			}
			close(removed)
			<-ctx.Done()
			<-finish
			reconcile, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			defer cancel()
			return a.s.MarkFileDeleted(reconcile, f.ID)
		})
	}()
	select {
	case <-removed:
	case e := <-operation:
		t.Fatal(e)
	case <-time.After(time.Second):
		t.Fatal("unlink timeout")
	}
	closed := make(chan error, 1)
	go func() { closed <- a.c.Close() }()
	deadline := time.Now().Add(time.Second)
	for a.c.Ready() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	select {
	case e := <-closed:
		t.Fatal("closed before reconciliation", e)
	default:
	}
	if _, e := a.c.Create(context.Background(), "late", a.root, "movie", nil); !errors.Is(e, model.ErrClosed) {
		t.Fatal("accepted during close", e)
	}
	close(finish)
	if e = <-operation; e != nil {
		t.Fatal(e)
	}
	if e = <-closed; e != nil {
		t.Fatal(e)
	}
	got, e := a.s.MediaFile(context.Background(), f.ID)
	if e != nil || got.State != "deleted" {
		t.Fatal(got, e)
	}
	if _, e = os.Stat(filepath.Join(a.root, "tv", "Show/01.mp4")); !os.IsNotExist(e) {
		t.Fatal("video not deleted", e)
	}
}
func TestConcurrentTaskSubmissionAndClose(t *testing.T) {
	a := setup(t)
	l := a.create(t, "tv")
	var wg sync.WaitGroup
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := a.c.StartScan(context.Background(), l.ID)
			if e != nil && !errors.Is(e, model.ErrBusy) && !errors.Is(e, model.ErrClosed) && !errors.Is(e, context.Canceled) {
				t.Error(e)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if e := a.c.Close(); e != nil {
			t.Error(e)
		}
	}()
	wg.Wait()
	if e := a.c.Close(); e != nil {
		t.Fatal(e)
	}
}
func TestCatalogCrashHelper(t *testing.T) {
	if os.Getenv("REELNEST_CATALOG_CRASH_HELPER") != "1" {
		t.Skip("subprocess only")
	}
	ctx := context.Background()
	s, e := store.Open(ctx, os.Getenv("REELNEST_TEST_DATA"))
	if e != nil {
		t.Fatal(e)
	}
	i, e := identity.New(ctx, s, "family", "a-long-test-password")
	if e != nil {
		t.Fatal(e)
	}
	token, _, e := i.Login(ctx, "family", "a-long-test-password", "")
	if e != nil {
		t.Fatal(e)
	}
	l, e := s.CreateLibrary(ctx, model.Library{ID: "crash-library", Name: "Crash", Path: os.Getenv("REELNEST_TEST_MEDIA"), Identity: "synthetic", Kind: "movie", Sources: []string{}})
	if e != nil {
		t.Fatal(e)
	}
	scan, e := s.ClaimScan(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.StageScan(ctx, scan.ID, []model.ScanEntry{{Relative: "old.mp4", Fingerprint: "old", Size: 1, ModifiedAt: 1000}}); e != nil {
		t.Fatal(e)
	}
	if e = s.CommitScan(ctx, scan.ID); e != nil {
		t.Fatal(e)
	}
	files, e := s.MediaFiles(ctx, l.ID, "", 10)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveProgress(ctx, files[0].ID, model.Progress{Position: 1, Duration: 60}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Favorite(ctx, "media", files[0].ID, true); e != nil {
		t.Fatal(e)
	}
	pending, e := s.QueueScan(ctx, l.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ClaimScan(ctx); e != nil {
		t.Fatal(e)
	}
	if e = s.StageScan(ctx, pending.ID, []model.ScanEntry{{Relative: "new.mp4", Fingerprint: "new", Size: 1, ModifiedAt: 1000}}); e != nil {
		t.Fatal(e)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"token": token, "scan": pending.ID, "file": files[0].ID})
	os.Exit(0) // Real process death: intentionally no Close/checkpoint/recovery.
}
func TestCrashRecoveryInterruptsStagingWithoutRescanOrAccountReset(t *testing.T) {
	data, root := t.TempDir(), t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestCatalogCrashHelper$")
	cmd.Env = append(os.Environ(), "REELNEST_CATALOG_CRASH_HELPER=1", "REELNEST_TEST_DATA="+data, "REELNEST_TEST_MEDIA="+root)
	b, e := cmd.Output()
	if e != nil {
		t.Fatal(e)
	}
	var result map[string]string
	if e = json.Unmarshal(b, &result); e != nil {
		t.Fatal(e, string(b))
	}
	a := open(t, data, root)
	ctx := context.Background()
	i, e := identity.New(ctx, a.s, "changed", "bad")
	if e != nil {
		t.Fatal(e)
	}
	if name, e := i.Session(ctx, result["token"]); e != nil || name != "family" {
		t.Fatal("account/session lost", name, e)
	}
	scan, e := a.s.Scan(ctx, result["scan"])
	if e != nil || scan.State != "interrupted" {
		t.Fatal(scan, e)
	}
	files, e := a.s.MediaFiles(ctx, "crash-library", "", 10)
	if e != nil || len(files) != 1 || files[0].ID != result["file"] {
		t.Fatal("partial scan published", files, e)
	}
	progress, e := a.s.Progress(ctx, result["file"])
	if e != nil || progress == nil || progress.Position != 1 {
		t.Fatal(progress, e)
	}
	// No real media exists: any automatic rescan would delete the persisted file.
	time.Sleep(25 * time.Millisecond)
	files, e = a.s.MediaFiles(ctx, "crash-library", "", 10)
	if e != nil || len(files) != 1 {
		t.Fatal("automatic rescan on startup", files, e)
	}
	if _, e = store.Open(ctx, data); !errors.Is(e, store.ErrDataDirLocked) {
		t.Fatal("concurrent DATA_DIR opened", e)
	}
}

func TestPartialDirectoryReadFailureKeepsPublishedFilesAndState(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission failure requires non-root")
	}
	a := setup(t)
	write(t, filepath.Join(a.root, "tv", "Show/01.mp4"))
	l := a.create(t, "tv")
	ctx := context.Background()
	before, e := a.s.MediaFiles(ctx, l.ID, "", 10)
	if e != nil {
		t.Fatal(e)
	}
	progress, e := a.s.SaveProgress(ctx, before[0].ID, model.Progress{Position: 3, Duration: 60})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.s.Favorite(ctx, "media", before[0].ID, true); e != nil {
		t.Fatal(e)
	}
	for n := 0; n < 300; n++ {
		write(t, filepath.Join(a.root, "tv", "Show", fmt.Sprintf("%03d.mp4", n+2)))
	}
	denied := filepath.Join(a.root, "tv", "unreadable")
	write(t, filepath.Join(denied, "file.mp4"))
	if e = os.Chmod(denied, 0000); e != nil {
		t.Fatal(e)
	}
	defer os.Chmod(denied, 0700)
	scan := startScan(t, a.c, l.ID)
	waitScan(t, a.s, scan.ID, func(scan model.Scan) bool { return scan.State == "failed" })
	after, e := a.s.MediaFiles(ctx, l.ID, "", 256)
	if e != nil || len(after) != 1 || after[0].ID != before[0].ID {
		t.Fatal("partial failed snapshot published", after, e)
	}
	saved, e := a.s.Progress(ctx, before[0].ID)
	if e != nil || saved == nil || *saved != progress {
		t.Fatal("failed scan lost progress", saved, e)
	}
	if e = os.Chmod(denied, 0700); e != nil {
		t.Fatal(e)
	}
	scan = startScan(t, a.c, l.ID)
	complete(t, a.s, scan.ID)
	after, e = a.s.MediaFiles(ctx, l.ID, "", 256)
	if e != nil || len(after) != 256 {
		t.Fatal("manual retry failed", len(after), e)
	}
}
