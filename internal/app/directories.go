package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type Directory struct {
	MetadataSources []string `json:"metadataSources"`
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Path            string   `json:"path"`
	Kind            string   `json:"kind"`
	ScanStatus      string   `json:"scanStatus"`
	ScanError       string   `json:"scanError"`
	ScanStarted     int64    `json:"scanStarted"`
	ScanFinished    int64    `json:"scanFinished"`
	Identity        string   `json:"-"`
}

const directoryColumns = `id, path, kind, scan_status, scan_error, scan_started, scan_finished, identity, metadata_sources, name`

type scanner interface{ Scan(...any) error }

func scanDirectory(row scanner) (Directory, error) {
	var d Directory
	var sources string
	err := row.Scan(&d.ID, &d.Path, &d.Kind, &d.ScanStatus, &d.ScanError, &d.ScanStarted, &d.ScanFinished, &d.Identity, &sources, &d.Name)
	if err == nil {
		err = json.Unmarshal([]byte(sources), &d.MetadataSources)
	}
	return d, err
}

func newID() string {
	bytes := make([]byte, 16)
	// crypto/rand terminates the process on failure in Go 1.25.
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func within(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}

func (a *App) initializeLibrary() error {
	dataPath, err := filepath.EvalSymlinks(a.config.DataDir)
	if err != nil {
		return err
	}
	dataPath, err = filepath.Abs(dataPath)
	if err != nil {
		return err
	}
	for index, path := range a.config.MediaRoots {
		if !filepath.IsAbs(path) {
			return errors.New("MEDIA_ROOTS 只能包含绝对路径")
		}
		path = filepath.Clean(path)
		// Resolve trusted deployment aliases once, not user-supplied directory paths.
		resolved, err := filepath.EvalSymlinks(path)
		// Preserve the configured spelling (e.g. macOS /var) accepted by the
		// directory API, but check resolved aliases for application-data overlap.
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("检查 MEDIA_ROOTS: %w", err)
		}
		if within(path, dataPath) || within(dataPath, path) || (err == nil && (within(resolved, dataPath) || within(dataPath, resolved))) {
			return errors.New("媒体根目录不能与应用数据目录重叠")
		}
		a.config.MediaRoots[index] = path
	}
	_, err = a.db.Exec(`
 CREATE TABLE IF NOT EXISTS media_directories (
  id TEXT PRIMARY KEY, path TEXT NOT NULL UNIQUE,
  kind TEXT NOT NULL CHECK(kind IN ('movie','series')),
  scan_status TEXT NOT NULL, scan_error TEXT NOT NULL DEFAULT '',
  scan_started INTEGER NOT NULL DEFAULT 0, scan_finished INTEGER NOT NULL DEFAULT 0,
  identity TEXT NOT NULL
 );
 CREATE TABLE IF NOT EXISTS media_files (
  id TEXT PRIMARY KEY, directory_id TEXT NOT NULL REFERENCES media_directories(id),
  relative_path TEXT NOT NULL, filename TEXT NOT NULL, size INTEGER NOT NULL,
  modified_at INTEGER NOT NULL, fingerprint TEXT NOT NULL, available INTEGER NOT NULL,
  UNIQUE(directory_id, relative_path, fingerprint)
 );
 CREATE INDEX IF NOT EXISTS media_files_directory ON media_files(directory_id);
 UPDATE media_directories SET scan_status='interrupted', scan_error='扫描因应用重启中断，请手动重新扫描' WHERE scan_status IN ('pending','running');`)
	if err != nil {
		return err
	}
	if err = a.initializeDirectorySources(); err != nil {
		return err
	}
	return a.initializeDirectoryNames()
}

// Upgrade offline libraries without opening their source directories or
// rebuilding file identities. Keep the schema change and backfill atomic.
func (a *App) initializeDirectoryNames() error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRow("SELECT COUNT(*) FROM pragma_table_info('media_directories') WHERE name='name'").Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		if _, err = tx.Exec("ALTER TABLE media_directories ADD COLUMN name TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	rows, err := tx.Query("SELECT id,path FROM media_directories WHERE name=''")
	if err != nil {
		return err
	}
	var legacy []Directory
	for rows.Next() {
		var d Directory
		if err = rows.Scan(&d.ID, &d.Path); err != nil {
			rows.Close()
			return err
		}
		legacy = append(legacy, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, d := range legacy {
		name := strings.TrimSpace(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, filepath.Base(d.Path)))
		if name == "" {
			name = "媒体库"
		}
		if runes := []rune(name); len(runes) > 80 {
			name = string(runes[:80])
		}
		if _, err = tx.Exec("UPDATE media_directories SET name=? WHERE id=?", name, d.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func validDirectoryName(name string) bool {
	return name != "" && utf8.RuneCountInString(name) <= 80 && strings.IndexFunc(name, unicode.IsControl) < 0
}

// openDirectory grants a bounded filesystem handle, not an arbitrary server path.
// All components below the deployment-selected root must be real directories.
func (a *App) openDirectory(path string) (*os.Root, os.FileInfo, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, nil, errors.New("请填写规范的绝对目录路径")
	}
	for _, allowed := range a.config.MediaRoots {
		if !within(allowed, path) {
			continue
		}
		info, err := os.Lstat(allowed)
		if err != nil {
			return nil, nil, fmt.Errorf("媒体根目录不可访问，请检查挂载和权限: %w", err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, nil, errors.New("媒体根目录必须为真实目录，不能是符号链接")
		}
		root, err := os.OpenRoot(allowed)
		if err != nil {
			return nil, nil, fmt.Errorf("无法打开媒体根目录: %w", err)
		}

		openedRoot, err := root.Stat(".")
		if err != nil || !os.SameFile(info, openedRoot) {
			root.Close()
			return nil, nil, errors.New("媒体根目录在检查期间发生变化")
		}
		relative, _ := filepath.Rel(allowed, path)
		if relative != "." {
			for _, part := range strings.Split(relative, string(os.PathSeparator)) {
				info, err := root.Lstat(part)
				if err != nil {
					root.Close()
					return nil, nil, fmt.Errorf("媒体目录不可访问: %w", err)
				}
				if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
					root.Close()
					return nil, nil, errors.New("媒体目录路径不能包含符号链接或非目录对象")
				}
				child, err := root.OpenRoot(part)
				if err != nil {
					root.Close()
					return nil, nil, fmt.Errorf("无法打开媒体目录: %w", err)
				}
				opened, err := child.Stat(".")
				root.Close()
				if err != nil || !os.SameFile(info, opened) {
					child.Close()
					return nil, nil, errors.New("媒体目录在检查期间发生变化")
				}
				root = child
			}
		}
		info, err = root.Stat(".")
		if err != nil {
			root.Close()
			return nil, nil, err
		}
		return root, info, nil
	}
	return nil, nil, errors.New("目录不在部署者配置的 MEDIA_ROOTS 范围内")
}

func (a *App) listDirectories(ctx context.Context) ([]Directory, error) {
	rows, err := a.db.QueryContext(ctx, "SELECT "+directoryColumns+" FROM media_directories ORDER BY name, path, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	directories := []Directory{}
	for rows.Next() {
		directory, err := scanDirectory(rows)
		if err != nil {
			return nil, err
		}
		directories = append(directories, directory)
	}
	return directories, rows.Err()
}

func (a *App) directories(w http.ResponseWriter, r *http.Request) {
	directories, err := a.listDirectories(r.Context())
	if err != nil {
		fail(w, 500, "无法读取媒体目录")
		return
	}
	roots := append([]string{}, a.config.MediaRoots...)
	respond(w, 200, map[string]any{"directories": directories, "allowedRoots": roots, "metadataSources": a.metadata.sources()})
}

func (a *App) addDirectory(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name            string   `json:"name"`
		Path            string   `json:"path"`
		MetadataSources []string `json:"metadataSources"`
		Kind            string   `json:"kind"`
	}
	if !decode(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if !validDirectoryName(input.Name) {
		fail(w, 400, "媒体库名称须为 1–80 个字符，不能包含控制字符")
		return
	}
	if input.Kind != "movie" && input.Kind != "series" {
		fail(w, 400, "目录类型必须为电影或电视剧")
		return
	}
	if input.MetadataSources == nil {
		input.MetadataSources = []string{"tmdb", "tvdb"}
	}
	if !validMetadataSources(input.MetadataSources) {
		fail(w, 400, "资料源只能是 tmdb/tvdb/metatube，不能重复")
		return
	}
	root, info, err := a.openDirectory(input.Path)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	root.Close()
	a.directoryMu.Lock()
	defer a.directoryMu.Unlock()
	rows, err := a.db.QueryContext(r.Context(), "SELECT path FROM media_directories")
	if err != nil {
		fail(w, 500, "无法读取媒体目录")
		return
	}
	overlap := false
	for rows.Next() {
		var existing string
		if err = rows.Scan(&existing); err != nil {
			break
		}
		if within(existing, input.Path) || within(input.Path, existing) {
			overlap = true
		}
	}
	rowErr := rows.Err()
	rows.Close()
	if err != nil || rowErr != nil {
		fail(w, 500, "无法读取媒体目录")
		return
	}
	if overlap {
		fail(w, 409, "该目录已接入或与已接入目录重叠，请直接重新扫描")
		return
	}
	d := Directory{MetadataSources: input.MetadataSources, ID: newID(), Name: input.Name, Path: input.Path, Kind: input.Kind, ScanStatus: "pending", Identity: fileIdentity(info)}
	sources, _ := json.Marshal(d.MetadataSources)
	_, err = a.db.ExecContext(r.Context(), "INSERT INTO media_directories(id,path,kind,scan_status,identity,metadata_sources,name) VALUES(?,?,?,?,?,?,?)", d.ID, d.Path, d.Kind, d.ScanStatus, d.Identity, string(sources), d.Name)
	if err != nil {
		fail(w, 500, "保存媒体目录失败")
		return
	}
	if err = a.startScan(d); err != nil {
		fail(w, 500, "目录已保存，但启动扫描失败，请重试")
		return
	}
	d.ScanStatus = "running"
	respond(w, 201, d)
}

func (a *App) renameDirectory(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if !validDirectoryName(input.Name) {
		fail(w, 400, "媒体库名称须为 1–80 个字符，不能包含控制字符")
		return
	}
	result, err := a.db.ExecContext(r.Context(), "UPDATE media_directories SET name=? WHERE id=?", input.Name, r.PathValue("id"))
	if err != nil {
		fail(w, 500, "无法保存媒体库名称")
		return
	}
	count, err := result.RowsAffected()
	if err != nil {
		fail(w, 500, "无法保存媒体库名称")
		return
	}
	if count == 0 {
		fail(w, 404, "媒体库不存在")
		return
	}
	respond(w, 200, map[string]string{"name": input.Name})
}

var errScanRunning = errors.New("该目录正在扫描，请等待当前扫描结束")

func (a *App) startScan(d Directory) error {
	result, err := a.db.Exec("UPDATE media_directories SET scan_status='running',scan_error='',scan_started=?,scan_finished=0 WHERE id=? AND scan_status<>'running'", time.Now().Unix(), d.ID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return errScanRunning
	}
	a.workers.Add(1)
	go func() { defer a.workers.Done(); a.scan(d) }()
	return nil
}

func (a *App) rescan(w http.ResponseWriter, r *http.Request) {
	var input struct{}
	if !decode(w, r, &input) {
		return
	}
	d, err := scanDirectory(a.db.QueryRowContext(r.Context(), "SELECT "+directoryColumns+" FROM media_directories WHERE id=?", r.PathValue("id")))
	if err != nil {
		fail(w, 404, "媒体目录不存在")
		return
	}
	if err = a.startScan(d); err != nil {
		if errors.Is(err, errScanRunning) {
			fail(w, 409, err.Error())
		} else {
			fail(w, 500, "无法启动扫描")
		}
		return
	}
	respond(w, 202, map[string]string{"scanStatus": "running"})
}
