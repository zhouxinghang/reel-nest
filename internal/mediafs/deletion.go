package mediafs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Observation is confirmation material, NOT authorization. The playback caller
// must sign it with an expiry, require household auth and explicit confirmation,
// and hold the library mutation lease through unlink and index reconciliation.
type Observation struct{ Path, ParentIdentity, Version string }
type Outcome string

const (
	NotDeleted Outcome = "not_deleted"
	Deleted    Outcome = "deleted"
	Unknown    Outcome = "unknown"
)

type deletionTarget struct {
	parent, file *os.File
	observed     Observation
}

func (t *deletionTarget) close() { t.file.Close(); t.parent.Close() }
func deletionVersion(st *unix.Stat_t) string {
	return fmt.Sprintf("%d:%d:%d:%d:%d:%d:%d:%d", st.Dev, st.Ino, st.Mode, st.Size, st.Mtim.Sec, st.Mtim.Nsec, st.Ctim.Sec, st.Ctim.Nsec)
}
func (f *FS) deletionTarget(ref Ref) (*deletionTarget, error) {
	if !IsVideo(ref.Relative) {
		return nil, ErrPath
	}
	parent, e := f.parent(ref)
	if e != nil {
		return nil, e
	}
	file, e := openChecked(parent, filepath.Base(ref.Relative), ref.Fingerprint)
	if e != nil {
		parent.Close()
		return nil, e
	}
	t := &deletionTarget{parent: parent, file: file}
	var ps, st unix.Stat_t
	if e = unix.Fstat(int(parent.Fd()), &ps); e == nil {
		e = unix.Fstat(int(file.Fd()), &st)
	}
	if e != nil {
		t.close()
		return nil, e
	}
	t.observed = Observation{filepath.Join(ref.Root.Path, ref.Relative), fmt.Sprintf("%d:%d", ps.Dev, ps.Ino), deletionVersion(&st)}
	return t, nil
}
func (f *FS) ObserveDeletion(ref Ref) (Observation, error) {
	t, e := f.deletionTarget(ref)
	if e != nil {
		return Observation{}, e
	}
	defer t.close()
	return t.observed, nil
}

// Unlink never removes directories or follows symlinks. After Deleted or Unknown
// callers must not retry automatically, even if the request is cancelled or DB
// reconciliation fails. Host-side concurrent writers remain outside our lock.
func (f *FS) Unlink(ctx context.Context, ref Ref, expected Observation) (Outcome, error) {
	if e := ctx.Err(); e != nil {
		return NotDeleted, e
	}
	target, e := f.deletionTarget(ref)
	if e != nil {
		return NotDeleted, e
	}
	defer target.close()
	if target.observed != expected {
		return NotDeleted, ErrChanged
	}
	current, e := f.deletionTarget(ref)
	if e != nil {
		return NotDeleted, e
	}
	defer current.close()
	if current.observed != expected {
		return NotDeleted, ErrChanged
	}
	var st unix.Stat_t
	name := filepath.Base(ref.Relative)
	if e = unix.Fstatat(int(current.parent.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW); e != nil {
		return NotDeleted, e
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || deletionVersion(&st) != expected.Version {
		return NotDeleted, ErrChanged
	}
	if e = ctx.Err(); e != nil {
		return NotDeleted, e
	}
	if e = unix.Unlinkat(int(current.parent.Fd()), name, 0); e != nil {
		if errors.Is(e, unix.EACCES) || errors.Is(e, unix.EPERM) || errors.Is(e, unix.EROFS) || errors.Is(e, unix.ENOENT) {
			return NotDeleted, e
		}
		return Unknown, e
	}
	return Deleted, nil
}
