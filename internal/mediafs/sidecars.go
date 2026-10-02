package mediafs

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/sys/unix"
)

type Sidecar struct {
	Name, Fingerprint, Format string
	Size                      int64
}

var subtitleLanguage = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func belongs(video, name string) bool {
	if filepath.Base(name) != name {
		return false
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".srt" && ext != ".vtt" {
		return false
	}
	videoStem := strings.TrimSuffix(filepath.Base(video), filepath.Ext(video))
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	return stem == videoStem || strings.HasPrefix(stem, videoStem+".") && subtitleLanguage.MatchString(strings.TrimPrefix(stem, videoStem+"."))
}

// Sidecars only enumerates siblings of the checked video, in bounded batches.
// Decoding and the 2 MiB content limit belong to playback, not this module.
func (f *FS) Sidecars(ctx context.Context, ref Ref, emit func([]Sidecar) error) error {
	if !IsVideo(ref.Relative) {
		return ErrPath
	}
	parent, e := f.parent(ref)
	if e != nil {
		return e
	}
	defer parent.Close()
	video, e := openChecked(parent, filepath.Base(ref.Relative), ref.Fingerprint)
	if e != nil {
		return e
	}
	video.Close()
	for {
		if e := ctx.Err(); e != nil {
			return e
		}
		entries, readErr := parent.ReadDir(BatchSize)
		if readErr != nil && readErr != io.EOF {
			return readErr
		}
		batch := make([]Sidecar, 0, len(entries))
		for _, entry := range entries {
			if !belongs(ref.Relative, entry.Name()) {
				continue
			}
			var st unix.Stat_t
			if e := unix.Fstatat(int(parent.Fd()), entry.Name(), &st, unix.AT_SYMLINK_NOFOLLOW); e != nil {
				return e
			}
			if st.Mode&unix.S_IFMT != unix.S_IFREG {
				continue
			}
			file, e := child(parent, entry.Name(), false)
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
			batch = append(batch, Sidecar{entry.Name(), fingerprint(info), strings.TrimPrefix(strings.ToLower(filepath.Ext(entry.Name())), "."), info.Size()})
		}
		if len(batch) > 0 {
			if e := emit(batch); e != nil {
				return e
			}
		}
		if readErr == io.EOF {
			// Reject a moved/replaced parent even if the pinned old handle can
			// still enumerate it. The video is checked again through this parent.
			current, e := f.parent(ref)
			if e != nil {
				return e
			}
			defer current.Close()
			before, e := parent.Stat()
			if e != nil {
				return e
			}
			after, e := current.Stat()
			if e != nil {
				return e
			}
			if !os.SameFile(before, after) {
				return ErrChanged
			}
			video, e := openChecked(current, filepath.Base(ref.Relative), ref.Fingerprint)
			if e != nil {
				return e
			}
			video.Close()
			return ctx.Err()
		}
	}
}
func (f *FS) OpenSubtitle(ref Ref, sidecar Sidecar) (*os.File, error) {
	if !IsVideo(ref.Relative) || !belongs(ref.Relative, sidecar.Name) {
		return nil, ErrPath
	}
	parent, e := f.parent(ref)
	if e != nil {
		return nil, e
	}
	defer parent.Close()
	video, e := openChecked(parent, filepath.Base(ref.Relative), ref.Fingerprint)
	if e != nil {
		return nil, e
	}
	video.Close()
	return openChecked(parent, sidecar.Name, sidecar.Fingerprint)
}
