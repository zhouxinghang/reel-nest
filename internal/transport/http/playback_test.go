package http_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"reelnest/internal/identity"
	"reelnest/internal/model"
)

func decode[T any](t *testing.T, b []byte) T {
	t.Helper()
	var value T
	if e := json.Unmarshal(b, &value); e != nil {
		t.Fatal(e, string(b))
	}
	return value
}
func mediaBase(id string) string { return "/api/media/" + id }
func fixtureFiles(t *testing.T, h *harness, root, kind string) (string, map[string]model.MediaFile) {
	t.Helper()
	library, scan := h.create(t, root, kind)
	h.await(t, scan, "completed")
	// Published state is observable just before the worker releases its lease.
	deadline := time.Now().Add(time.Second)
	for {
		e := h.catalog.WithMutation(context.Background(), library, func(context.Context) error { return nil })
		if e == nil {
			break
		}
		if !errors.Is(e, model.ErrBusy) || time.Now().After(deadline) {
			t.Fatal(e)
		}
		time.Sleep(time.Millisecond)
	}
	files, e := h.store.MediaFiles(context.Background(), library, "", 100)
	if e != nil {
		t.Fatal(e)
	}
	result := map[string]model.MediaFile{}
	for _, f := range files {
		result[f.Relative] = f
	}
	return library, result
}
func content(t *testing.T, h *harness, client *http.Client, method, path string, status int, headers ...string) ([]byte, http.Header) {
	t.Helper()
	r, e := http.NewRequest(method, h.server.URL+path, nil)
	if e != nil {
		t.Fatal(e)
	}
	for n := 0; n < len(headers); n += 2 {
		r.Header.Set(headers[n], headers[n+1])
	}
	resp, e := client.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	b, e := io.ReadAll(resp.Body)
	if e != nil {
		t.Fatal(e)
	}
	if resp.StatusCode != status {
		t.Fatalf("%s %s: %d want %d: %s", method, path, resp.StatusCode, status, b)
	}
	if e = h.contract.ValidateResponse(context.Background(), r, status, resp.Header, io.NopCloser(bytes.NewReader(b))); e != nil {
		t.Fatalf("response contract: %v", e)
	}
	return b, resp.Header
}
func errorCode(t *testing.T, b []byte, code string) {
	t.Helper()
	v := decode[struct {
		Error struct {
			Code      string
			Retryable bool
		}
	}](t, b)
	if v.Error.Code != code || v.Error.Retryable {
		t.Fatal(string(b))
	}
}
func TestPlaybackRangesIfRangeAndSharedProgressContract(t *testing.T) {
	h := start(t)
	h.login(t)
	original := "0123456789中文 & video bytes"
	name := "中文 空格 ? & #.webm"
	writeFile(t, filepath.Join(h.root, name), original)
	writeFile(t, filepath.Join(h.root, "other.webm"), "other version")
	_, files := fixtureFiles(t, h, h.root, "movie")
	id := files[name].ID
	base := mediaBase(id)
	preflight := decode[struct {
		MediaID, ContentPath string
		Progress             *model.Progress
	}](t, h.request(t, "GET", base+"/playback", "", 200))
	if preflight.MediaID != id || preflight.ContentPath != base+"/content" || preflight.Progress != nil {
		t.Fatal(preflight)
	}
	content(t, h, http.DefaultClient, "GET", base+"/content", 401)
	b, head := content(t, h, h.client, "HEAD", base+"/content", 200)
	if len(b) != 0 || head.Get("Content-Length") != strconv.Itoa(len(original)) || head.Get("Content-Type") != "video/webm" || head.Get("Accept-Ranges") != "bytes" {
		t.Fatal(head, string(b))
	}
	b, _ = content(t, h, h.client, "GET", base+"/content", 200)
	if string(b) != original {
		t.Fatal(string(b))
	}
	for _, tc := range []struct {
		rangeValue, want string
		status           int
	}{
		{"bytes=2-5", "2345", 206}, {"bytes=8-", original[8:], 206}, {"bytes=-5", "bytes", 206}, {"bytes=0-999", original, 206},
		{"bytes=999-", "", 416}, {"bytes=5-2", "", 416}, {"bytes=wat", "", 416},
	} {
		b, headers := content(t, h, h.client, "GET", base+"/content", tc.status, "Range", tc.rangeValue)
		if tc.status == 206 && (string(b) != tc.want || headers.Get("Content-Range") == "") {
			t.Fatal(tc, string(b), headers)
		}
	}
	b, headers := content(t, h, h.client, "HEAD", base+"/content", 206, "Range", "bytes=2-5")
	if len(b) != 0 || headers.Get("Content-Length") != "4" {
		t.Fatal(headers, string(b))
	}
	b, headers = content(t, h, h.client, "GET", base+"/content", 206, "Range", "bytes=0-1,8-9")
	typ, params, e := mime.ParseMediaType(headers.Get("Content-Type"))
	if e != nil || typ != "multipart/byteranges" {
		t.Fatal(typ, e)
	}
	parts := multipart.NewReader(bytes.NewReader(b), params["boundary"])
	for _, want := range []string{"01", "89"} {
		p, e := parts.NextPart()
		if e != nil {
			t.Fatal(e)
		}
		data, _ := io.ReadAll(p)
		if string(data) != want {
			t.Fatal(string(data))
		}
	}
	if _, e = parts.NextPart(); e != io.EOF {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		validator, want string
		status          int
	}{
		{head.Get("ETag"), "2345", 206}, {head.Get("Last-Modified"), "2345", 206}, {`"other"`, original, 200}, {"W/" + head.Get("ETag"), original, 200}, {"Mon, 01 Jan 1990 00:00:00 GMT", original, 200},
	} {
		b, _ = content(t, h, h.client, "GET", base+"/content", tc.status, "Range", "bytes=2-5", "If-Range", tc.validator)
		if string(b) != tc.want {
			t.Fatal(tc, string(b))
		}
	}
	content(t, h, h.client, "GET", base+"/content", 304, "If-None-Match", head.Get("ETag"))
	content(t, h, h.client, "HEAD", base+"/content", 304, "If-Modified-Since", head.Get("Last-Modified"))
	content(t, h, h.client, "GET", base+"/content", 412, "If-Match", `"other"`)
	content(t, h, h.client, "HEAD", base+"/content", 412, "If-Unmodified-Since", "Mon, 01 Jan 1990 00:00:00 GMT")
	for _, body := range []string{
		`{"position":-1,"duration":60,"completed":false}`, `{"position":61,"duration":60,"completed":false}`, `{"position":5,"duration":60,"completed":true}`,
		`{"position":0,"duration":0,"completed":false}`, `{"position":0,"duration":60}`, `{"position":null,"duration":60,"completed":false}`,
		`{"position":NaN,"duration":60,"completed":false}`, `{"position":0,"duration":60,"completed":false,"updatedAt":1}`, `{"position":0,"duration":60,"completed":false}{}`,
	} {
		errorCode(t, h.request(t, "PUT", base+"/progress", body, 400), "INVALID_REQUEST")
	}
	saved := decode[model.Progress](t, h.request(t, "PUT", base+"/progress", `{"position":12.5,"duration":60,"completed":false}`, 200))
	if saved.Position != 12.5 || saved.UpdatedAt <= 0 {
		t.Fatal(saved)
	}
	first := h.client
	jar, _ := cookiejar.New(nil)
	h.client = &http.Client{Jar: jar, Timeout: 15 * time.Second}
	h.login(t)
	shared := decode[struct{ Progress *model.Progress }](t, h.request(t, "GET", base+"/progress", "", 200))
	if shared.Progress == nil || *shared.Progress != saved {
		t.Fatal(shared)
	}
	h.request(t, "PUT", base+"/progress", `{"position":60,"duration":60,"completed":true}`, 200)
	h.client = first
	shared = decode[struct{ Progress *model.Progress }](t, h.request(t, "GET", base+"/progress", "", 200))
	if !shared.Progress.Completed {
		t.Fatal(shared)
	}
	// Replaying from zero clears completion; another file never inherits progress.
	h.request(t, "PUT", base+"/progress", `{"position":0,"duration":60,"completed":false}`, 200)
	other := h.request(t, "GET", mediaBase(files["other.webm"].ID)+"/progress", "", 200)
	if string(other) != "{\"progress\":null}\n" {
		t.Fatal(string(other))
	}
	errorCode(t, h.request(t, "PUT", base+"/progress", `{"position":1,"duration":60,"completed":false}`, 403, "Origin", "https://hostile.invalid"), "FORBIDDEN")
}

func TestStreamTokensCannotGrantSessionOrMutationAccess(t *testing.T) {
	h := start(t)
	h.login(t)
	writeFile(t, filepath.Join(h.root, "one.webm"), "0123456789 video")
	writeFile(t, filepath.Join(h.root, "two.webm"), "other")
	lib, files := fixtureFiles(t, h, h.root, "movie")
	id := files["one.webm"].ID
	other := files["two.webm"].ID
	base := mediaBase(id)
	link := decode[struct{ Path string }](t, h.request(t, "POST", base+"/stream-link", "{}", 200, "X-Forwarded-Host", "hostile.invalid", "X-Forwarded-Proto", "https")).Path
	if !strings.HasPrefix(link, "/stream/"+id+"?token=") || strings.Contains(link, "hostile") {
		t.Fatal(link)
	}
	u, _ := url.Parse(link)
	token := u.Query().Get("token")
	for _, bad := range []string{"/stream/" + id, "/stream/" + id + "?token=bad", link + "&token=" + token, strings.Replace(link, id, other, 1), strings.Replace(link, token, strings.Repeat("0", 64), 1)} {
		content(t, h, h.client, "GET", bad, 401) // Even with the real household cookie.
	}
	b, _ := content(t, h, http.DefaultClient, "GET", link, 206, "Range", "bytes=2-5")
	if string(b) != "2345" {
		t.Fatal(string(b))
	}
	content(t, h, http.DefaultClient, "HEAD", link, 200)
	content(t, h, http.DefaultClient, "GET", link, 200, "Range", "bytes=2-5", "If-Range", `"wrong"`)
	content(t, h, http.DefaultClient, "GET", link, 206, "Range", "bytes=0-1,8-9")
	logged := h.client
	h.client = http.DefaultClient
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/api/session", ""}, {"GET", base, ""}, {"GET", base + "/content", ""}, {"GET", base + "/progress", ""}, {"GET", base + "/subtitles", ""}, {"GET", base + "/subtitles/unknown", ""},
		{"POST", base + "/stream-link", "{}"}, {"POST", base + "/deletion-preview", "{}"}, {"POST", base + "/delete", `{"confirmation":"x","confirm":true}`},
		{"PUT", base + "/progress", `{"position":1,"duration":60,"completed":false}`}, {"PUT", "/api/favorites/media/" + id, `{"favorite":true}`}, {"POST", "/api/libraries/" + lib + "/scans", "{}"},
	} {
		h.request(t, tc.method, tc.path+"?token="+token, tc.body, 401, "Cookie", identity.CookieName+"="+token)
	}
	h.client = logged
	if b = h.request(t, "GET", base+"/progress", "", 200); string(b) != "{\"progress\":null}\n" {
		t.Fatal("stream wrote progress", string(b))
	}
	h.request(t, "POST", "/api/logout", "{}", 200)
	content(t, h, http.DefaultClient, "GET", link, 200)
}

type preview struct {
	MediaID, Path, Confirmation, Warning string
	ExpiresAt                            int64
}

func previewFile(t *testing.T, h *harness, id string) preview {
	t.Helper()
	return decode[preview](t, h.request(t, "POST", mediaBase(id)+"/deletion-preview", "{}", 200))
}
func deletionBody(p preview) string {
	b, _ := json.Marshal(map[string]any{"confirmation": p.Confirmation, "confirm": true})
	return string(b)
}
func assertBytes(t *testing.T, path, want string) {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil || string(b) != want {
		t.Fatal(path, string(b), e)
	}
}
func TestDeletionOnlyOneVideoAndSuccessfulScanClearsDependents(t *testing.T) {
	h := start(t)
	h.login(t)
	name := "Show/Season 1/01.webm"
	path := filepath.Join(h.root, name)
	writeFile(t, path, "original")
	keep := map[string]string{"Show/Season 1/01.srt": "subtitle", "Show/Season 1/poster.jpg": "image", "Show/Season 1/01.1080p.webm": "other version", "Show/Season 1/02.webm": "other episode"}
	for name, b := range keep {
		writeFile(t, filepath.Join(h.root, name), b)
	}
	lib, files := fixtureFiles(t, h, h.root, "series")
	f := files[name]
	base := mediaBase(f.ID)
	h.request(t, "PUT", base+"/progress", `{"position":23,"duration":60,"completed":false}`, 200)
	h.request(t, "PUT", "/api/favorites/media/"+f.ID, `{"favorite":true}`, 200)
	link := decode[struct{ Path string }](t, h.request(t, "POST", base+"/stream-link", "{}", 200)).Path
	p := previewFile(t, h, f.ID)
	canonical, _ := filepath.EvalSymlinks(path)
	if p.Path != canonical || p.MediaID != f.ID || p.ExpiresAt <= time.Now().UnixMilli() || p.ExpiresAt > time.Now().Add(10*time.Minute).UnixMilli() || !strings.Contains(p.Warning, "本系统无法恢复") {
		t.Fatal(p)
	}
	h.request(t, "POST", base+"/delete", "{}", 400)
	h.request(t, "POST", base+"/delete", `{"confirmation":`+strconv.Quote(p.Confirmation)+`,"confirm":false}`, 400)
	assertBytes(t, path, "original")
	for _, token := range []string{"bad", p.Confirmation + "x", strings.Split(p.Confirmation, ".")[0] + "." + strings.Repeat("0", 64)} {
		pp := p
		pp.Confirmation = token
		errorCode(t, h.request(t, "POST", base+"/delete", deletionBody(pp), 409), "DELETION_TARGET_CHANGED")
	}
	expired := p
	expired.Confirmation = "1." + strings.Repeat("0", 64)
	errorCode(t, h.request(t, "POST", base+"/delete", deletionBody(expired), 410), "DELETION_PREVIEW_EXPIRED")
	h.request(t, "POST", mediaBase(files["Show/Season 1/02.webm"].ID)+"/delete", deletionBody(p), 409)
	h.request(t, "POST", base+"/delete", deletionBody(p), 403, "Origin", "https://hostile.invalid")
	b := h.request(t, "POST", base+"/delete", deletionBody(p), 200)
	result := decode[struct {
		FileOutcome, IndexOutcome, Message, RequestID string
		Retryable                                     bool
	}](t, b)
	if result.FileOutcome != "deleted" || result.IndexOutcome != "updated" || result.Retryable || result.RequestID == "" {
		t.Fatal(string(b))
	}
	if _, e := os.Lstat(path); !os.IsNotExist(e) {
		t.Fatal(e)
	}
	for name, want := range keep {
		assertBytes(t, filepath.Join(h.root, name), want)
	}
	detail, e := h.store.MediaFile(context.Background(), f.ID)
	if e != nil || detail.State != "deleted" {
		t.Fatal(detail, e)
	}
	for _, suffix := range []string{"/playback", "/content", "/subtitles"} {
		h.request(t, "GET", base+suffix, "", 410)
	}
	h.request(t, "POST", base+"/delete", deletionBody(p), 410)
	h.request(t, "PUT", base+"/progress", `{"position":24,"duration":60,"completed":false}`, 410)
	content(t, h, http.DefaultClient, "GET", link, 410)
	saved := decode[struct{ Progress model.Progress }](t, h.request(t, "GET", base+"/progress", "", 200))
	if saved.Progress.Position != 23 {
		t.Fatal(saved)
	}
	scan := decode[model.Scan](t, h.request(t, "POST", "/api/libraries/"+lib+"/scans", "{}", 202))
	h.await(t, scan.ID, "completed")
	h.request(t, "GET", base, "", 404)
	h.request(t, "GET", base+"/progress", "", 404)
	content(t, h, http.DefaultClient, "GET", link, 404)
	for name, want := range keep {
		assertBytes(t, filepath.Join(h.root, name), want)
	}
}

func TestPlaybackAndDeletionRejectUnsafeFileAndParentChanges(t *testing.T) {
	h := start(t)
	h.login(t)
	for _, scenario := range []string{"missing", "replacement", "in-place", "restored-mtime", "directory", "fifo", "symlink", "parent-symlink", "moved-parent", "root-replaced", "root-missing", "no-read", "no-write"} {
		t.Run(scenario, func(t *testing.T) {
			root := filepath.Join(h.root, scenario)
			path := filepath.Join(root, "parent", "one.mp4")
			writeFile(t, path, "original")
			_, files := fixtureFiles(t, h, root, "movie")
			f := files["parent/one.mp4"]
			base := mediaBase(f.ID)
			p := previewFile(t, h, f.ID)
			must := func(e error) {
				t.Helper()
				if e != nil {
					t.Fatal(e)
				}
			}
			readStatus, deleteStatus := 409, 409
			switch scenario {
			case "missing":
				must(os.Remove(path))
				readStatus, deleteStatus = 410, 410
			case "replacement":
				must(os.Rename(path, path+".old"))
				writeFile(t, path, "replacement")
			case "in-place":
				writeFile(t, path, "edited different contents")
			case "restored-mtime":
				info, e := os.Stat(path)
				must(e)
				writeFile(t, path, "modified")
				must(os.Chtimes(path, info.ModTime(), info.ModTime()))
				readStatus = 200
			case "directory":
				must(os.Rename(path, path+".old"))
				must(os.Mkdir(path, 0700))
			case "fifo":
				must(os.Rename(path, path+".old"))
				must(syscall.Mkfifo(path, 0600))
			case "symlink":
				must(os.Rename(path, path+".old"))
				must(os.Symlink(path+".old", path))
			case "parent-symlink":
				must(os.Rename(filepath.Dir(path), filepath.Join(root, "old")))
				must(os.Symlink(filepath.Join(root, "old"), filepath.Dir(path)))
			case "moved-parent":
				must(os.Rename(filepath.Dir(path), filepath.Join(root, "old")))
				must(os.Mkdir(filepath.Dir(path), 0700))
				must(os.Link(filepath.Join(root, "old", "one.mp4"), path))
				readStatus = 200
			case "root-replaced":
				must(os.Rename(root, root+"-old"))
				writeFile(t, path, "replacement")
			case "root-missing":
				must(os.Rename(root, root+"-old"))
				readStatus, deleteStatus = 410, 410
			case "no-read":
				if os.Geteuid() == 0 {
					t.Skip("non-root permission test")
				}
				must(os.Chmod(path, 0))
				defer os.Chmod(path, 0600)
			case "no-write":
				if os.Geteuid() == 0 {
					t.Skip("non-root permission test")
				}
				must(os.Chmod(filepath.Dir(path), 0500))
				defer os.Chmod(filepath.Dir(path), 0700)
				readStatus = 200
			}
			h.request(t, "GET", base+"/playback", "", readStatus)
			content(t, h, h.client, "GET", base+"/content", readStatus)
			h.request(t, "POST", base+"/delete", deletionBody(p), deleteStatus)
			current, e := h.store.MediaFile(context.Background(), f.ID)
			if e != nil || current.State != "available" {
				t.Fatal(current, e)
			}
		})
	}
}

func TestDeletionIndexFailureIsNotRetryableAndScanRepairs(t *testing.T) {
	h := start(t)
	h.login(t)
	path := filepath.Join(h.root, "one.mp4")
	writeFile(t, path, "temporary")
	lib, files := fixtureFiles(t, h, h.root, "movie")
	id := files["one.mp4"].ID
	p := previewFile(t, h, id)
	db, e := sql.Open("sqlite", filepath.Join(h.data, "reelnest.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	// Real SQLite failure after unlink, not a mocked successful deletion.
	if _, e = db.Exec(`CREATE TRIGGER reject_delete_projection BEFORE UPDATE ON catalog_entries BEGIN SELECT RAISE(ABORT,'isolated projection fault'); END`); e != nil {
		t.Fatal(e)
	}
	result := decode[struct {
		FileOutcome, IndexOutcome string
		Retryable                 bool
	}](t, h.request(t, "POST", mediaBase(id)+"/delete", deletionBody(p), 200))
	if result.FileOutcome != "deleted" || result.IndexOutcome != "pending" || result.Retryable {
		t.Fatal(result)
	}
	if _, e = os.Stat(path); !os.IsNotExist(e) {
		t.Fatal(e)
	}
	f, e := h.store.MediaFile(context.Background(), id)
	if e != nil || f.State != "available" {
		t.Fatal("failed transaction partially committed", f, e)
	}
	h.request(t, "GET", mediaBase(id)+"/playback", "", 410)
	if _, e = db.Exec(`DROP TRIGGER reject_delete_projection`); e != nil {
		t.Fatal(e)
	}
	scan := decode[model.Scan](t, h.request(t, "POST", "/api/libraries/"+lib+"/scans", "{}", 202))
	h.await(t, scan.ID, "completed")
	h.request(t, "GET", mediaBase(id), "", 404)
}

func TestBusyDeletionDoesNotBlockReadsProgressOrOtherLibrary(t *testing.T) {
	h := start(t)
	h.login(t)
	writeFile(t, filepath.Join(h.root, "first", "one.mp4"), "one")
	writeFile(t, filepath.Join(h.root, "second", "two.mp4"), "two")
	first, a := fixtureFiles(t, h, filepath.Join(h.root, "first"), "movie")
	_, b := fixtureFiles(t, h, filepath.Join(h.root, "second"), "movie")
	id := a["one.mp4"].ID
	other := b["two.mp4"].ID
	p := previewFile(t, h, id)
	held, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	defer func() {
		close(release)
		if e := <-done; e != nil {
			t.Error(e)
		}
	}()
	go func() {
		done <- h.catalog.WithMutation(context.Background(), first, func(context.Context) error { close(held); <-release; return nil })
	}()
	<-held
	errorCode(t, h.request(t, "POST", mediaBase(id)+"/delete", deletionBody(p), 409), "LIBRARY_BUSY")
	errorCode(t, h.request(t, "POST", mediaBase(id)+"/deletion-preview", "{}", 409), "LIBRARY_BUSY")
	h.request(t, "GET", mediaBase(id)+"/playback", "", 200)
	h.request(t, "PUT", mediaBase(id)+"/progress", `{"position":1,"duration":60,"completed":false}`, 200)
	h.request(t, "POST", mediaBase(other)+"/delete", deletionBody(previewFile(t, h, other)), 200)
}

type subtitleItem struct{ ID, Filename, Format string }
type subtitlePage struct {
	Items []subtitleItem
	Page  struct {
		NextCursor *string
		ExpiresAt  *int64
	}
}

func TestSubtitleContractConversionPaginationAndChanges(t *testing.T) {
	h := start(t)
	h.login(t)
	name := "电影 & #"
	writeFile(t, filepath.Join(h.root, name+".webm"), "original video")
	writeFile(t, filepath.Join(h.root, "other.webm"), "other video")
	samples := []struct {
		name, text, want string
		status           int
	}{
		{"en.srt", "\ufeff1\r\n00:00:01,000 --> 00:00:03,500\r\nHello <i>home</i>\r\n", "00:00:01.000 --> 00:00:03.500\nHello <i>home</i>", 200},
		{"zh.vtt", "\ufeffWEBVTT 中文\r\n\r\nNOTE fixture\r\ncomment\r\n\r\n标题\r\n00:01.000 --> 00:03.500 align:center position:50%\r\n你好，家人\r\n", "你好，家人", 200},
		{"cr.srt", "1\r00:00:01,000 --> 00:00:02,000\rCR only\r", "CR only", 200},
		{"overlap.vtt", "WEBVTT\n\n00:00.000 --> 00:05.000\nfirst\n\n00:01.000 --> 00:04.000\nsecond", "second", 200},
		{"badtime.srt", "1\n00:00:99,000 --> 00:00:02,000\nwrong", "SUBTITLE_INVALID", 422},
		{"reverse.vtt", "WEBVTT\n\n00:03.000 --> 00:01.000\nwrong", "SUBTITLE_INVALID", 422},
		{"order.vtt", "WEBVTT\n\n00:03.000 --> 00:04.000\na\n\n00:01.000 --> 00:02.000\nb", "SUBTITLE_INVALID", 422},
		{"encoding.srt", string([]byte{0xff, 0xfe, 0x31, 0}), "SUBTITLE_INVALID", 422},
		{"nul.vtt", "WEBVTT\n\n00:00.000 --> 00:01.000\n\x00", "SUBTITLE_INVALID", 422},
		{"header.vtt", "00:00.000 --> 00:01.000\nno header", "SUBTITLE_INVALID", 422},
		{"separator.srt", "1\n00:00:00,000 --> 00:00:01,000\na\n2\n00:00:02,000 --> 00:00:03,000\nb", "SUBTITLE_INVALID", 422},
		{"empty.srt", "", "SUBTITLE_INVALID", 422},
		{"body.srt", "1\n00:00:00,000 --> 00:00:01,000", "SUBTITLE_INVALID", 422},
		{"settings.vtt", "WEBVTT\n\n00:00.000 --> 00:01.000 position:101%\na", "SUBTITLE_INVALID", 422},
		{"style.vtt", "WEBVTT\n\nSTYLE\n::cue {color:red}\n\n00:00.000 --> 00:01.000\na", "SUBTITLE_INVALID", 422},
		{"huge.srt", strings.Repeat("a", (2<<20)+1), "SUBTITLE_INVALID", 422},
	}
	for _, s := range samples {
		writeFile(t, filepath.Join(h.root, name+"."+s.name), s.text)
	}
	writeFile(t, filepath.Join(h.root, name+"extra.vtt"), "unrelated")
	writeFile(t, filepath.Join(h.root, name+".zh.forced.vtt"), "outside documented naming")
	writeFile(t, filepath.Join(h.root, name+".ass"), "unsupported")
	if e := os.Symlink(filepath.Join(h.root, name+".en.srt"), filepath.Join(h.root, name+".link.srt")); e != nil {
		t.Fatal(e)
	}
	_, files := fixtureFiles(t, h, h.root, "movie")
	id := files[name+".webm"].ID
	other := files["other.webm"].ID
	base := mediaBase(id)
	p := decode[subtitlePage](t, h.request(t, "GET", base+"/subtitles", "", 200))
	if len(p.Items) != len(samples) || p.Page.NextCursor != nil {
		t.Fatal(p)
	}
	ids := map[string]string{}
	for _, item := range p.Items {
		ids[item.Filename] = item.ID
	}
	for _, s := range samples {
		t.Run(s.name, func(t *testing.T) {
			path := base + "/subtitles/" + ids[name+"."+s.name]
			b, headers := content(t, h, h.client, "GET", path, s.status)
			if !strings.Contains(string(b), s.want) {
				t.Fatal(string(b))
			}
			if s.status == 200 && (headers.Get("Content-Type") != "text/vtt; charset=utf-8" || !bytes.HasPrefix(b, []byte("WEBVTT\n\n"))) {
				t.Fatal(headers, string(b))
			}
			assertBytes(t, filepath.Join(h.root, name+"."+s.name), s.text)
		})
	}
	h.request(t, "GET", mediaBase(other)+"/subtitles/"+ids[name+".en.srt"], "", 404)
	for n := 0; n < 205; n++ {
		writeFile(t, filepath.Join(h.root, fmt.Sprintf("%s.%03d.srt", name, n)), "1\n00:00:00,000 --> 00:00:01,000\nfixture")
	}
	initial := decode[subtitlePage](t, h.request(t, "GET", base+"/subtitles?limit=100", "", 200))
	if len(initial.Items) != 100 || initial.Page.NextCursor == nil || initial.Page.ExpiresAt == nil || *initial.Page.ExpiresAt > time.Now().Add(5*time.Minute).UnixMilli() {
		t.Fatal(initial)
	}
	all := map[string]bool{}
	last := ""
	page := initial
	for {
		if len(page.Items) > 100 {
			t.Fatal("unbounded page")
		}
		for _, item := range page.Items {
			if all[item.ID] || item.Filename <= last {
				t.Fatal("duplicate/out-of-order", item)
			}
			all[item.ID] = true
			last = item.Filename
		}
		if page.Page.NextCursor == nil {
			break
		}
		page = decode[subtitlePage](t, h.request(t, "GET", base+"/subtitles?limit=100&cursor="+*page.Page.NextCursor, "", 200))
	}
	if len(all) != 205+len(samples) {
		t.Fatal(len(all))
	}
	cursor := *initial.Page.NextCursor
	for _, path := range []string{base + "/subtitles?limit=50&cursor=" + cursor, mediaBase(other) + "/subtitles?limit=100&cursor=" + cursor, base + "/subtitles?limit=100&cursor=" + cursor + "x"} {
		errorCode(t, h.request(t, "GET", path, "", 409), "CURSOR_STALE")
	}
	writeFile(t, filepath.Join(h.root, name+".en.srt"), "1\n00:00:00,000 --> 00:00:01,000\nChanged")
	h.request(t, "GET", base+"/subtitles/"+ids[name+".en.srt"], "", 404)
	errorCode(t, h.request(t, "GET", base+"/subtitles?limit=100&cursor="+cursor, "", 409), "CURSOR_STALE")
	h.request(t, "GET", base+"/subtitles?limit=101", "", 400)
	assertBytes(t, filepath.Join(h.root, name+".webm"), "original video")
}

func TestRescanReplacementCannotInheritStreamConfirmationOrProgress(t *testing.T) {
	h := start(t)
	h.login(t)
	path := filepath.Join(h.root, "one.webm")
	writeFile(t, path, "original")
	lib, files := fixtureFiles(t, h, h.root, "movie")
	id := files["one.webm"].ID
	base := mediaBase(id)
	link := decode[struct{ Path string }](t, h.request(t, "POST", base+"/stream-link", "{}", 200)).Path
	p := previewFile(t, h, id)
	h.request(t, "PUT", base+"/progress", `{"position":23,"duration":60,"completed":false}`, 200)
	if e := os.Rename(path, path+".old"); e != nil {
		t.Fatal(e)
	}
	writeFile(t, path, "replacement must not be exposed")
	errorCode(t, h.request(t, "GET", base+"/playback", "", 409), "FILE_CHANGED")
	content(t, h, http.DefaultClient, "GET", link, 409)
	h.request(t, "POST", base+"/delete", deletionBody(p), 409)
	scan := decode[model.Scan](t, h.request(t, "POST", "/api/libraries/"+lib+"/scans", "{}", 202))
	h.await(t, scan.ID, "completed")
	content(t, h, http.DefaultClient, "GET", link, 404)
	h.request(t, "GET", base+"/progress", "", 404)
	current, e := h.store.MediaFiles(context.Background(), lib, "", 10)
	if e != nil || len(current) != 1 || current[0].ID == id {
		t.Fatal(current, e)
	}
	next := mediaBase(current[0].ID)
	h.request(t, "POST", next+"/delete", deletionBody(p), 409)
	b := h.request(t, "GET", next+"/progress", "", 200)
	if string(b) != "{\"progress\":null}\n" {
		t.Fatal(string(b))
	}
	content(t, h, http.DefaultClient, "GET", strings.Replace(link, id, current[0].ID, 1), 401)
	assertBytes(t, path, "replacement must not be exposed")
}
