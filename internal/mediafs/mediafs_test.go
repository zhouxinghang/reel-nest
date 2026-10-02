package mediafs_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
	"reelnest/internal/mediafs"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, []byte(content), 0600); e != nil {
		t.Fatal(e)
	}
}
func setup(t *testing.T) (*mediafs.FS, mediafs.Root, string) {
	t.Helper()
	dir := t.TempDir()
	fs, e := mediafs.New([]string{dir}, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	root, e := fs.Inspect(dir)
	if e != nil {
		t.Fatal(e)
	}
	return fs, root, dir
}
func refs(t *testing.T, fs *mediafs.FS, root mediafs.Root) map[string]mediafs.Ref {
	t.Helper()
	m := map[string]mediafs.Ref{}
	e := fs.Walk(context.Background(), root, func(entries []mediafs.Entry) error {
		if len(entries) > 256 {
			t.Fatal("unbounded batch")
		}
		for _, f := range entries {
			m[f.Relative] = mediafs.Ref{Root: root, Relative: f.Relative, Fingerprint: f.Fingerprint}
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func TestBoundedWalkCheckedVideoAndSidecars(t *testing.T) {
	fs, root, dir := setup(t)
	for n := 0; n < 1031; n++ {
		write(t, filepath.Join(dir, fmt.Sprintf("%04d.mp4", n)), "video")
	}
	write(t, filepath.Join(dir, "0000.zh.srt"), "subtitle")
	write(t, filepath.Join(dir, "0000.vtt"), "WEBVTT")
	write(t, filepath.Join(dir, "unrelated.srt"), "other")
	write(t, filepath.Join(dir, "0000.en.srt"), "will replace")
	outside := filepath.Join(t.TempDir(), "outside.mp4")
	write(t, outside, "private")
	if e := os.Symlink(outside, filepath.Join(dir, "link.mp4")); e != nil {
		t.Fatal(e)
	}
	if e := unix.Mkfifo(filepath.Join(dir, "fifo.mp4"), 0600); e != nil {
		t.Fatal(e)
	}
	calls, maxBatch, count := 0, 0, 0
	e := fs.Walk(context.Background(), root, func(entries []mediafs.Entry) error {
		calls++
		count += len(entries)
		maxBatch = max(maxBatch, len(entries))
		return nil
	})
	if e != nil || count != 1031 || calls != 5 || maxBatch != 256 {
		t.Fatalf("not bounded: %d %d %d %v", count, calls, maxBatch, e)
	}
	ref := refs(t, fs, root)["0000.mp4"]
	file, e := fs.OpenVideo(ref)
	if e != nil {
		t.Fatal(e)
	}
	// The capability remains the exact checked file, not a later path open.
	if e = os.Rename(filepath.Join(dir, "0000.mp4"), filepath.Join(dir, "old.mp4")); e != nil {
		t.Fatal(e)
	}
	write(t, filepath.Join(dir, "0000.mp4"), "replacement")
	b, e := io.ReadAll(file)
	file.Close()
	if e != nil || string(b) != "video" {
		t.Fatal("checked handle changed", string(b), e)
	}
	if file, e = fs.OpenVideo(ref); e == nil {
		file.Close()
		t.Fatal("read replacement through old identity")
	}
	ref = refs(t, fs, root)["0000.mp4"]
	var sidecars []mediafs.Sidecar
	if e = fs.Sidecars(context.Background(), ref, func(s []mediafs.Sidecar) error { sidecars = append(sidecars, s...); return nil }); e != nil {
		t.Fatal(e)
	}
	if len(sidecars) != 3 {
		t.Fatalf("wrong association: %+v", sidecars)
	}
	for _, sub := range sidecars {
		f, e := fs.OpenSubtitle(ref, sub)
		if e != nil {
			t.Fatal(e)
		}
		f.Close()
	}
	bad := sidecars[0]
	bad.Name = "../0000.vtt"
	if f, e := fs.OpenSubtitle(ref, bad); e == nil {
		f.Close()
		t.Fatal("subtitle traversal")
	}
	sub := sidecars[0]
	if e = os.Remove(filepath.Join(dir, sub.Name)); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(outside, filepath.Join(dir, sub.Name)); e != nil {
		t.Fatal(e)
	}
	if f, e := fs.OpenSubtitle(ref, sub); e == nil {
		f.Close()
		t.Fatal("subtitle symlink")
	}
}
func TestContainmentReplacementSpecialFilesAndDataOverlap(t *testing.T) {
	fs, root, dir := setup(t)
	write(t, filepath.Join(dir, "nested", "movie.mp4"), "original")
	ref := refs(t, fs, root)["nested/movie.mp4"]
	for _, relative := range []string{"../outside.mp4", "/tmp/outside.mp4", "nested/../nested/movie.mp4", "nested", "nested/movie.txt"} {
		bad := ref
		bad.Relative = relative
		if f, e := fs.OpenVideo(bad); e == nil {
			f.Close()
			t.Fatalf("accepted %s", relative)
		}
	}
	outside := t.TempDir()
	write(t, filepath.Join(outside, "movie.mp4"), "external")
	if _, e := fs.Inspect(outside); e == nil {
		t.Fatal("outside root accepted")
	}
	if e := os.Symlink(outside, filepath.Join(dir, "alias")); e != nil {
		t.Fatal(e)
	}
	if _, e := fs.Inspect(filepath.Join(dir, "alias")); e == nil {
		t.Fatal("symlink library accepted")
	}
	if e := os.Rename(filepath.Join(dir, "nested"), filepath.Join(dir, "moved")); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(outside, filepath.Join(dir, "nested")); e != nil {
		t.Fatal(e)
	}
	if f, e := fs.OpenVideo(ref); e == nil {
		f.Close()
		t.Fatal("parent replacement read")
	}
	if e := os.Rename(dir, dir+"-moved"); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.RemoveAll(dir + "-moved") })
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	if e := fs.CheckRoot(root); e == nil {
		t.Fatal("root replacement accepted")
	}
	if _, e := mediafs.New([]string{dir}, filepath.Join(dir)); e == nil {
		t.Fatal("DATA_DIR overlap")
	}
	if _, e := mediafs.New([]string{filepath.Dir(dir)}, dir); e == nil {
		t.Fatal("DATA_DIR ancestor overlap")
	}
	if _, e := mediafs.New([]string{filepath.Join(dir, "child")}, dir); e == nil {
		t.Fatal("DATA_DIR child overlap")
	}
}
func TestWalkAbortsReadFailureCancellationAndMovedDirectory(t *testing.T) {
	fs, root, dir := setup(t)
	for n := 0; n < 300; n++ {
		write(t, filepath.Join(dir, "child", fmt.Sprintf("%03d.mp4", n)), "v")
	}
	expected := errors.New("staging failed")
	if e := fs.Walk(context.Background(), root, func([]mediafs.Entry) error { return expected }); !errors.Is(e, expected) {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if e := fs.Walk(ctx, root, func([]mediafs.Entry) error { cancel(); return nil }); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	moved := false
	e := fs.Walk(context.Background(), root, func([]mediafs.Entry) error {
		if !moved {
			moved = true
			return os.Rename(filepath.Join(dir, "child"), filepath.Join(dir, "moved"))
		}
		return nil
	})
	if e == nil {
		t.Fatal("moved directory published")
	}
	if os.Geteuid() != 0 {
		if e = os.Chmod(filepath.Join(dir, "moved"), 0000); e != nil {
			t.Fatal(e)
		}
		defer os.Chmod(filepath.Join(dir, "moved"), 0700)
		if e = fs.Walk(context.Background(), root, func([]mediafs.Entry) error { return nil }); e == nil {
			t.Fatal("unreadable directory silently skipped")
		}
	}
}
func TestExactUnlinkCancellationParentVersionAndOnlyOneVideo(t *testing.T) {
	fs, root, dir := setup(t)
	path := filepath.Join(dir, "show", "movie.mp4")
	write(t, path, "video")
	write(t, filepath.Join(dir, "show", "movie.srt"), "subtitle")
	write(t, filepath.Join(dir, "show", "poster.jpg"), "poster")
	write(t, filepath.Join(dir, "show", "other.mp4"), "video")
	ref := refs(t, fs, root)["show/movie.mp4"]
	observed, e := fs.ObserveDeletion(ref)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if outcome, e := fs.Unlink(ctx, ref, observed); outcome != mediafs.NotDeleted || !errors.Is(e, context.Canceled) {
		t.Fatal(outcome, e)
	}
	if e := os.Rename(filepath.Join(dir, "show"), filepath.Join(dir, "original")); e != nil {
		t.Fatal(e)
	}
	if e := os.Mkdir(filepath.Join(dir, "show"), 0700); e != nil {
		t.Fatal(e)
	}
	// A hard link has the same scan fingerprint, but a different parent and ctime.
	if e := os.Link(filepath.Join(dir, "original", "movie.mp4"), path); e != nil {
		t.Fatal(e)
	}
	if outcome, e := fs.Unlink(context.Background(), ref, observed); outcome != mediafs.NotDeleted || e == nil {
		t.Fatal("stale deletion succeeded", outcome, e)
	}
	os.Remove(path)
	os.Remove(filepath.Join(dir, "show"))
	os.Rename(filepath.Join(dir, "original"), filepath.Join(dir, "show"))
	observed, e = fs.ObserveDeletion(ref)
	if e != nil {
		t.Fatal(e)
	}
	stat, e := os.Stat(path)
	if e != nil {
		t.Fatal(e)
	}
	write(t, path, "VIDEO")
	if e = os.Chtimes(path, stat.ModTime(), stat.ModTime()); e != nil {
		t.Fatal(e)
	}
	if outcome, e := fs.Unlink(context.Background(), ref, observed); outcome != mediafs.NotDeleted || e == nil {
		t.Fatal("ctime change ignored", outcome, e)
	}
	observed, e = fs.ObserveDeletion(ref)
	if e != nil {
		t.Fatal(e)
	}
	if outcome, e := fs.Unlink(context.Background(), ref, observed); outcome != mediafs.Deleted || e != nil {
		t.Fatal(outcome, e)
	}
	entries, e := os.ReadDir(filepath.Dir(path))
	if e != nil {
		t.Fatal(e)
	}
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if !reflect.DeepEqual(names, []string{"movie.srt", "other.mp4", "poster.jpg"}) {
		t.Fatal(names)
	}
	if outcome, e := fs.Unlink(context.Background(), ref, observed); outcome != mediafs.NotDeleted || e == nil {
		t.Fatal("repeat unlink", outcome, e)
	}
}

func TestOpenRejectsFIFOAndUnlinkPermissionFailureDoesNotDelete(t *testing.T) {
	fs, root, dir := setup(t)
	path := filepath.Join(dir, "movie.mp4")
	write(t, path, "video")
	ref := refs(t, fs, root)["movie.mp4"]
	if e := os.Rename(path, path+".old"); e != nil {
		t.Fatal(e)
	}
	if e := unix.Mkfifo(path, 0600); e != nil {
		t.Fatal(e)
	}
	if f, e := fs.OpenVideo(ref); e == nil {
		f.Close()
		t.Fatal("opened special file")
	}
	os.Remove(path)
	os.Rename(path+".old", path)
	if os.Geteuid() == 0 {
		t.Skip("permission failure requires non-root")
	}
	observed, e := fs.ObserveDeletion(ref)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(dir, 0500); e != nil {
		t.Fatal(e)
	}
	defer os.Chmod(dir, 0700)
	outcome, e := fs.Unlink(context.Background(), ref, observed)
	if e == nil || outcome != mediafs.NotDeleted {
		t.Fatal(outcome, e)
	}
	if _, e = os.Stat(path); e != nil {
		t.Fatal("permission failure deleted video", e)
	}
}

func TestSidecarEnumerationRejectsMovedParentAndCancellation(t *testing.T) {
	fs, root, dir := setup(t)
	parent := filepath.Join(dir, "parent")
	write(t, filepath.Join(parent, "movie.mp4"), "video")
	write(t, filepath.Join(parent, "movie.srt"), "subtitle")
	ref := refs(t, fs, root)["parent/movie.mp4"]
	ctx, cancel := context.WithCancel(context.Background())
	e := fs.Sidecars(ctx, ref, func([]mediafs.Sidecar) error { cancel(); return nil })
	if !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	moved := false
	e = fs.Sidecars(context.Background(), ref, func([]mediafs.Sidecar) error {
		if moved {
			return nil
		}
		moved = true
		old := filepath.Join(dir, "old-parent")
		if e := os.Rename(parent, old); e != nil {
			return e
		}
		if e := os.Mkdir(parent, 0700); e != nil {
			return e
		}
		// Even when the same video inode can still be opened at the new pathname,
		// a listing from the previous pinned parent must not be returned as current.
		return os.Link(filepath.Join(old, "movie.mp4"), filepath.Join(parent, "movie.mp4"))
	})
	if !errors.Is(e, mediafs.ErrChanged) {
		t.Fatal(e)
	}
}
