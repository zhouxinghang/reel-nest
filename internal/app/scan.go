package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

var videoExtensions = map[string]bool{".mp4": true, ".m4v": true, ".mkv": true, ".mov": true, ".webm": true, ".avi": true, ".ts": true, ".m2ts": true, ".mpg": true, ".mpeg": true, ".wmv": true, ".flv": true}

type discoveredFile struct {
	relative string
	info     os.FileInfo
}

// collect visits actual directory handles, never symlinks, and only reads metadata.
// A failure anywhere aborts reconciliation; a partial scan cannot mark files absent.
func (a *App) collect(root *os.Root, prefix string, files *[]discoveredFile) error {
	if err := a.ctx.Err(); err != nil {
		return err
	}
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	entries, err := directory.ReadDir(-1)
	directory.Close()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := a.ctx.Err(); err != nil {
			return err
		}
		info, err := root.Lstat(entry.Name())
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		relative := filepath.Join(prefix, entry.Name())
		if info.IsDir() {
			child, err := root.OpenRoot(entry.Name())
			if err != nil {
				return err
			}
			opened, err := child.Stat(".")
			if err != nil || !os.SameFile(info, opened) {
				child.Close()
				return errors.New("扫描时子目录发生变化，请重新扫描")
			}
			err = a.collect(child, relative, files)
			child.Close()
			if err != nil {
				return err
			}
		} else if info.Mode().IsRegular() && videoExtensions[strings.ToLower(filepath.Ext(entry.Name()))] {
			file, err := root.OpenFile(entry.Name(), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
			if err != nil {
				return fmt.Errorf("无法读取媒体文件 %s: %w", relative, err)
			}
			opened, err := file.Stat()
			file.Close()
			if err != nil || !opened.Mode().IsRegular() || fileFingerprint(info) != fileFingerprint(opened) {
				return errors.New("扫描时媒体文件发生变化，请重新扫描")
			}
			*files = append(*files, discoveredFile{relative, opened})
		}
	}
	return nil
}

func (a *App) scan(d Directory) {
	err := a.scanFiles(d)
	if err == nil {
		return
	}
	status, message := "failed", fmt.Sprintf("扫描失败，保留已有媒体记录；请检查挂载和读取权限后重试：%s", err)
	if a.ctx.Err() != nil {
		status, message = "interrupted", "扫描因应用停止中断，请手动重新扫描"
	}
	// Close waits for workers, so this final status can still be persisted on shutdown.
	_, _ = a.db.Exec("UPDATE media_directories SET scan_status=?,scan_error=?,scan_finished=? WHERE id=?", status, message, time.Now().Unix(), d.ID)
}

func (a *App) scanFiles(d Directory) error {
	root, info, err := a.openDirectory(d.Path)
	if err != nil {
		return err
	}
	defer root.Close()
	if fileIdentity(info) != d.Identity {
		return errors.New("媒体目录已被替换或挂载发生变化，请恢复原媒体目录")
	}
	files := []discoveredFile{}
	if err = a.collect(root, "", &files); err != nil {
		return err
	}
	// Reopen the configured path to detect a vanished mount or replaced root.
	current, currentInfo, err := a.openDirectory(d.Path)
	if err != nil {
		return err
	}
	current.Close()
	if !os.SameFile(info, currentInfo) {
		return errors.New("扫描期间媒体根目录发生变化")
	}
	tx, err := a.db.BeginTx(a.ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(a.ctx, "UPDATE media_files SET available=0 WHERE directory_id=?", d.ID); err != nil {
		return err
	}
	for _, file := range files {
		var mediaID string
		err = tx.QueryRowContext(a.ctx, `INSERT INTO media_files(id,directory_id,relative_path,filename,size,modified_at,fingerprint,available)
   VALUES(?,?,?,?,?,?,?,1) ON CONFLICT(directory_id,relative_path,fingerprint) DO UPDATE SET available=1 RETURNING id`,
			newID(), d.ID, file.relative, file.info.Name(), file.info.Size(), file.info.ModTime().Unix(), fileFingerprint(file.info)).Scan(&mediaID)
		if err != nil {
			return err
		}
		if d.Kind == "series" {
			if err = a.organizeMedia(tx, mediaID, d.ID, file.relative); err != nil {
				return err
			}
		}
	}
	if _, err = tx.ExecContext(a.ctx, "UPDATE media_directories SET scan_status='completed',scan_error='',scan_finished=? WHERE id=?", time.Now().Unix(), d.ID); err != nil {
		return err
	}
	return tx.Commit()
}
