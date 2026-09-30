package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// A preview is a short-lived confirmation of one observed object, not an
// authorization credential. Both endpoints still require the household session.
const deletionPreviewLifetime = 10 * time.Minute

type deletionTarget struct {
	parent, file                      *os.File
	id, path, parentIdentity, version string
}

func (t *deletionTarget) close() {
	t.file.Close()
	t.parent.Close()
}

func deletionVersion(stat *unix.Stat_t) string {
	// ctime also catches in-place edits that restore size and mtime after preview.
	return fmt.Sprintf("%d:%d:%d:%d:%d:%d:%d:%d", stat.Dev, stat.Ino, stat.Mode, stat.Size,
		stat.Mtim.Sec, stat.Mtim.Nsec, stat.Ctim.Sec, stat.Ctim.Nsec)
}

func (a *App) openDeletionTarget(ctx context.Context, id string) (*deletionTarget, error) {
	root, file, err := a.openMedia(ctx, id)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	parent, err := root.Open(".")
	if err != nil {
		file.Close()
		return nil, &mediaProblem{409, "媒体父目录不可访问"}
	}
	target := &deletionTarget{parent: parent, file: file, id: id}
	var path, relative string
	err = a.db.QueryRowContext(ctx, `SELECT d.path,f.relative_path FROM media_files f
 JOIN media_directories d ON d.id=f.directory_id WHERE f.id=?`, id).Scan(&path, &relative)
	if err != nil {
		target.close()
		return nil, err
	}
	target.path = filepath.Join(path, relative)
	var parentStat, stat unix.Stat_t
	if err = unix.Fstat(int(parent.Fd()), &parentStat); err == nil {
		err = unix.Fstat(int(file.Fd()), &stat)
	}
	if err != nil {
		target.close()
		return nil, &mediaProblem{409, "无法核对媒体文件身份"}
	}
	target.parentIdentity = fmt.Sprintf("%d:%d", parentStat.Dev, parentStat.Ino)
	target.version = deletionVersion(&stat)
	return target, nil
}

func (a *App) deletionSignature(target *deletionTarget, expiry string) string {
	payload, _ := json.Marshal([]string{"reelnest:delete-confirmation:v1", expiry,
		target.id, target.path, target.parentIdentity, target.version})
	mac := hmac.New(sha256.New, a.streamKey)
	_, _ = mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

func reportDeletionProblem(w http.ResponseWriter, err error) {
	var problem *mediaProblem
	if errors.As(err, &problem) {
		fail(w, problem.status, "未删除："+problem.message)
	} else {
		fail(w, 500, "未删除：无法核对媒体状态，请检查应用数据存储")
	}
}

func (a *App) deletionPreview(w http.ResponseWriter, r *http.Request) {
	var input struct{}
	if !decode(w, r, &input) {
		return
	}
	a.mediaMutationMu.Lock()
	defer a.mediaMutationMu.Unlock()
	target, err := a.openDeletionTarget(r.Context(), r.PathValue("id"))
	if err != nil {
		reportDeletionProblem(w, err)
		return
	}
	defer target.close()
	expires := time.Now().Add(deletionPreviewLifetime).Unix()
	expiry := strconv.FormatInt(expires, 10)
	respond(w, 200, map[string]any{"path": target.path, "expiresAt": expires,
		"confirmation": expiry + "." + a.deletionSignature(target, expiry)})
}

func (a *App) deleteMedia(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Confirmation string `json:"confirmation"`
		Confirmed    bool   `json:"confirmed"`
	}
	if !decode(w, r, &input) {
		return
	}
	expiry, signature, ok := strings.Cut(input.Confirmation, ".")
	expires, err := strconv.ParseInt(expiry, 10, 64)
	now := time.Now().Unix()
	if !input.Confirmed || !ok || err != nil || len(signature) != 64 || expires <= now || expires > now+int64(deletionPreviewLifetime/time.Second) {
		fail(w, 409, "未删除：请重新打开确认框并核对目标，确认已失效或不完整")
		return
	}
	a.mediaMutationMu.Lock()
	defer a.mediaMutationMu.Unlock()
	target, err := a.openDeletionTarget(r.Context(), r.PathValue("id"))
	if err != nil {
		reportDeletionProblem(w, err)
		return
	}
	defer target.close()
	if !hmac.Equal([]byte(signature), []byte(a.deletionSignature(target, expiry))) {
		fail(w, 409, "未删除：确认对象已变化或确认无效，请重新核对目标")
		return
	}
	// Re-traverse the configured path immediately before unlink: a pinned handle
	// alone could refer to a parent that has since moved elsewhere or gone offline.
	current, err := a.openDeletionTarget(r.Context(), target.id)
	if err != nil {
		reportDeletionProblem(w, err)
		return
	}
	defer current.close()
	if !hmac.Equal([]byte(signature), []byte(a.deletionSignature(current, expiry))) {
		fail(w, 409, "未删除：删除前目标或父目录发生变化")
		return
	}
	var stat unix.Stat_t
	name := filepath.Base(current.path)
	err = unix.Fstatat(int(current.parent.Fd()), name, &stat, unix.AT_SYMLINK_NOFOLLOW)
	if err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || deletionVersion(&stat) != current.version {
		fail(w, 409, "未删除：最终文件核对失败，目标已变化或不可访问")
		return
	}
	if r.Context().Err() != nil {
		return // The destructive step has not started.
	}
	if time.Now().Unix() >= expires {
		fail(w, 409, "未删除：等待期间确认已过期，请重新核对目标")
		return
	}
	// Never use Remove/RemoveAll: unlinkat with flags=0 cannot remove a directory
	// or follow a final symlink, and the parent is a checked, bounded handle.
	// POSIX has no atomic unlink-if-inode; external writers must not race this
	// final check/unlink pair. See the deployment concurrency boundary in README.
	if err = unix.Unlinkat(int(current.parent.Fd()), name, 0); err != nil {
		status, reason := 409, ""
		switch {
		case errors.Is(err, unix.EACCES), errors.Is(err, unix.EPERM):
			status, reason = 403, "权限不足，请检查父目录写入/执行权限、ACL 或文件保护标志"
		case errors.Is(err, unix.EROFS):
			status, reason = 403, "媒体挂载为只读，不能删除源文件"
		case errors.Is(err, unix.ENOENT):
			status, reason = 410, "媒体文件已缺失，本次未执行成功删除"
		}
		if reason == "" {
			fail(w, status, "删除结果未确认：文件系统返回错误（"+err.Error()+"），索引未更新；请检查挂载并重新扫描核对，不要直接重试")
		} else {
			fail(w, status, "未删除："+reason+"（"+err.Error()+"）")
		}
		return
	}
	// An HTTP cancellation after unlink must not skip index reconciliation.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 10*time.Second)
	defer cancel()
	result, err := a.db.ExecContext(ctx, "UPDATE media_files SET available=0 WHERE id=?", target.id)
	if err == nil {
		var changed int64
		changed, err = result.RowsAffected()
		if err == nil && changed != 1 {
			err = errors.New("media record missing")
		}
	}
	message := "源文件已永久删除；本系统无法恢复。字幕、海报、其他视频及目录均未删除。"
	if err != nil {
		message = "源文件已永久删除，但索引未更新。本系统无法恢复；请修复应用数据存储后重新扫描校正，勿重复删除。"
	}
	// A partial outcome is intentionally structured, not a generic retryable 500.
	respond(w, 200, map[string]any{"deleted": true, "indexUpdated": err == nil, "message": message})
}
