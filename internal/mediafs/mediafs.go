// Package mediafs grants checked handles, never a validated path to reopen.
// Linux and macOS are supported. Host writers must not race final checks/unlink:
// POSIX does not provide atomic unlink-if-inode.
package mediafs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

var ErrChanged = errors.New("media path or identity changed")
var ErrPath = errors.New("media path is outside allowed roots or overlaps application data")

const BatchSize = 256

type allowedRoot struct{ spelling, canonical string }
type FS struct {
	roots []allowedRoot
	data  string
}
type Root struct{ Path, Identity string }
type Ref struct {
	Root                  Root
	Relative, Fingerprint string
}
type Entry struct {
	Relative, Fingerprint string
	Size, ModifiedAt      int64
}

// New resolves trusted deployment aliases once. User-supplied paths and every
// subsequently opened component must be real directories, not symlinks.
func New(roots []string, dataDir string) (*FS, error) {
	data, err := filepath.EvalSymlinks(dataDir)
	if err != nil {
		return nil, err
	}
	data, err = filepath.Abs(data)
	if err != nil {
		return nil, err
	}
	f := &FS{data: data}
	for _, path := range roots {
		if !filepath.IsAbs(path) {
			return nil, ErrPath
		}
		path = filepath.Clean(path)
		info, e := os.Lstat(path)
		if e == nil && !info.IsDir() {
			return nil, ErrPath
		}
		canonical, e := resolveDeploymentPath(path)
		if e != nil {
			return nil, e
		}
		if overlaps(canonical, data) || overlaps(path, data) {
			return nil, ErrPath
		}
		f.roots = append(f.roots, allowedRoot{path, canonical})
	}
	return f, nil
}

// Resolve the existing trusted prefix even when the mount/root is offline.
// Otherwise aliases such as /var could bypass DATA_DIR overlap checks.
func resolveDeploymentPath(path string) (string, error) {
	resolved, e := filepath.EvalSymlinks(path)
	if e == nil {
		return resolved, nil
	}
	if !os.IsNotExist(e) || path == "/" {
		return "", e
	}
	parent, e := resolveDeploymentPath(filepath.Dir(path))
	if e != nil {
		return "", e
	}
	return filepath.Join(parent, filepath.Base(path)), nil
}
func within(root, path string) bool {
	rel, e := filepath.Rel(root, path)
	return e == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}
func overlaps(a, b string) bool { return within(a, b) || within(b, a) }
func identity(info os.FileInfo) string {
	st := info.Sys().(*syscall.Stat_t)
	return fmt.Sprintf("%d:%d", st.Dev, st.Ino)
}
func fingerprint(info os.FileInfo) string {
	return fmt.Sprintf("%s:%d:%d", identity(info), info.Size(), info.ModTime().UnixNano())
}
func IsVideo(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4", ".m4v", ".mkv", ".mov", ".webm", ".avi", ".ts", ".m2ts", ".mpg", ".mpeg", ".wmv", ".flv":
		return true
	}
	return false
}
func local(path string) bool {
	return path != "." && filepath.IsLocal(path) && filepath.Clean(path) == path && !strings.ContainsRune(path, 0)
}
func child(parent *os.File, name string, directory bool) (*os.File, error) {
	flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC
	if directory {
		flags |= unix.O_DIRECTORY
	}
	fd, e := unix.Openat(int(parent.Fd()), name, flags, 0)
	if e != nil {
		return nil, e
	}
	return os.NewFile(uintptr(fd), name), nil
}
func absolute(path string) (*os.File, error) {
	fd, e := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, e
	}
	dir := os.NewFile(uintptr(fd), "/")
	for _, p := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if p == "" {
			continue
		}
		next, e := child(dir, p, true)
		dir.Close()
		if e != nil {
			return nil, e
		}
		dir = next
	}
	return dir, nil
}
func (f *FS) canonical(path string) (string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsRune(path, 0) {
		return "", ErrPath
	}
	for _, r := range f.roots {
		for _, base := range []string{r.spelling, r.canonical} {
			if within(base, path) {
				rel, _ := filepath.Rel(base, path)
				resolved := filepath.Join(r.canonical, rel)
				if overlaps(resolved, f.data) {
					return "", ErrPath
				}
				return resolved, nil
			}
		}
	}
	return "", ErrPath
}
func (f *FS) Inspect(path string) (Root, error) {
	path, e := f.canonical(path)
	if e != nil {
		return Root{}, e
	}
	dir, e := absolute(path)
	if e != nil {
		return Root{}, e
	}
	defer dir.Close()
	info, e := dir.Stat()
	if e != nil {
		return Root{}, e
	}
	return Root{path, identity(info)}, nil
}
func (f *FS) openRoot(ref Root) (*os.File, error) {
	path, e := f.canonical(ref.Path)
	if e != nil {
		return nil, e
	}
	dir, e := absolute(path)
	if e != nil {
		return nil, e
	}
	info, e := dir.Stat()
	if e != nil || identity(info) != ref.Identity {
		dir.Close()
		return nil, ErrChanged
	}
	return dir, nil
}
func (f *FS) CheckRoot(ref Root) error {
	dir, e := f.openRoot(ref)
	if e == nil {
		dir.Close()
	}
	return e
}
func (f *FS) parent(ref Ref) (*os.File, error) {
	if !local(ref.Relative) {
		return nil, ErrPath
	}
	dir, e := f.openRoot(ref.Root)
	if e != nil {
		return nil, e
	}
	parts := strings.Split(ref.Relative, "/")
	for _, p := range parts[:len(parts)-1] {
		next, e := child(dir, p, true)
		dir.Close()
		if e != nil {
			return nil, e
		}
		dir = next
	}
	return dir, nil
}
func openChecked(parent *os.File, name, version string) (*os.File, error) {
	file, e := child(parent, name, false)
	if e != nil {
		return nil, e
	}
	info, e := file.Stat()
	if e != nil || !info.Mode().IsRegular() || fingerprint(info) != version {
		file.Close()
		return nil, ErrChanged
	}
	return file, nil
}
func (f *FS) OpenVideo(ref Ref) (*os.File, error) {
	if !IsVideo(ref.Relative) {
		return nil, ErrPath
	}
	parent, e := f.parent(ref)
	if e != nil {
		return nil, e
	}
	defer parent.Close()
	return openChecked(parent, filepath.Base(ref.Relative), ref.Fingerprint)
}

// Walk streams <=256 files per callback and reads <=256 directory entries at a
// time. A failed callback/read/cancellation invalidates the entire snapshot.
func (f *FS) Walk(ctx context.Context, ref Root, emit func([]Entry) error) error {
	root, e := f.openRoot(ref)
	if e != nil {
		return e
	}
	defer root.Close()
	batch := make([]Entry, 0, BatchSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if e := emit(batch); e != nil {
			return e
		}
		batch = batch[:0]
		return ctx.Err()
	}
	var visit func(*os.File, string, int) error
	visit = func(dir *os.File, prefix string, depth int) error {
		if depth > 256 {
			return errors.New("media directory nesting limit exceeded")
		}
		before, e := dir.Stat()
		if e != nil {
			return e
		}
		for {
			if e := ctx.Err(); e != nil {
				return e
			}
			entries, readErr := dir.ReadDir(BatchSize)
			if readErr != nil && readErr != io.EOF {
				return readErr
			}
			for _, entry := range entries {
				if e := ctx.Err(); e != nil {
					return e
				}
				// Re-stat through the pinned directory, not DirEntry.Info's path.
				var st unix.Stat_t
				if e := unix.Fstatat(int(dir.Fd()), entry.Name(), &st, unix.AT_SYMLINK_NOFOLLOW); e != nil {
					return e
				}
				mode := st.Mode & unix.S_IFMT
				relative := filepath.Join(prefix, entry.Name())
				if mode == unix.S_IFDIR {
					next, e := child(dir, entry.Name(), true)
					if e != nil {
						return e
					}
					info, e := next.Stat()
					if e == nil && identity(info) != fmt.Sprintf("%d:%d", st.Dev, st.Ino) {
						e = ErrChanged
					}
					if e == nil {
						e = visit(next, relative, depth+1)
					}
					next.Close()
					if e != nil {
						return e
					}
				} else if mode == unix.S_IFREG && IsVideo(relative) {
					file, e := child(dir, entry.Name(), false)
					if e != nil {
						return e
					}
					info, e := file.Stat()
					file.Close()
					if e != nil {
						return e
					}
					if !info.Mode().IsRegular() || identity(info) != fmt.Sprintf("%d:%d", st.Dev, st.Ino) || info.Size() != st.Size || info.ModTime().UnixNano() != st.Mtim.Sec*1e9+st.Mtim.Nsec {
						return ErrChanged
					}
					batch = append(batch, Entry{relative, fingerprint(info), info.Size(), info.ModTime().UnixMilli()})
					if len(batch) == BatchSize {
						if e := flush(); e != nil {
							return e
						}
					}
				}
			}
			if readErr == io.EOF {
				break
			}
		}
		// Re-open each visited directory from the configured root to catch moves,
		// replacements and symlinks, even when our old handle is still readable.
		current, e := f.openRoot(ref)
		if e != nil {
			return e
		}
		if prefix != "" {
			for _, p := range strings.Split(prefix, "/") {
				next, e := child(current, p, true)
				current.Close()
				if e != nil {
					return e
				}
				current = next
			}
		}
		after, e := current.Stat()
		current.Close()
		if e != nil || !os.SameFile(before, after) {
			return ErrChanged
		}
		return nil
	}
	if e = visit(root, "", 0); e != nil {
		return e
	}
	if e = flush(); e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	return f.CheckRoot(ref)
}
