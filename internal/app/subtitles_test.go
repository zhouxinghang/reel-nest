package app_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reelnest/internal/app"
)

type subtitleItem struct{ ID, Filename, Format string }

func subtitles(t *testing.T, a *runningApp, id string) []subtitleItem {
	t.Helper()
	body, _ := request(t, a.client, "GET", a.server.URL+"/api/media/"+id+"/subtitles", nil, 200)
	var result struct{ Items []subtitleItem }
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	return result.Items
}
func TestSidecarSubtitlesConversionValidationAndNoSourceWrites(t *testing.T) {
	root := t.TempDir()
	writeMedia(t, filepath.Join(root, "电影 & #.webm"), "video remains unchanged")
	type sample struct {
		name, text, want string
		status           int
	}
	samples := []sample{
		{"en.srt", "\ufeff1\r\n00:00:01,000 --> 00:00:03,500\r\nHello <i>home</i>\r\n\r\n2\r\n00:00:04,000 --> 00:00:06,000\r\nNext line\r\n", "00:00:01.000 --> 00:00:03.500\nHello <i>home</i>", 200},
		{"zh.vtt", "\ufeffWEBVTT 中文\r\n\r\nNOTE fixture\r\ncomment\r\n\r\n标题\r\n00:01.000 --> 00:03.500 align:center position:50%\r\n你好，家人\r\n", "00:00:01.000 --> 00:00:03.500 align:center position:50%\n你好，家人", 200},
		{"cr.srt", "1\r00:00:01,000 --> 00:00:02,000\rCR only\r", "CR only", 200},
		{"lf.srt", "1\n00:00:01,000 --> 00:00:02,000\nLF only", "LF only", 200},
		{"overlap.vtt", "WEBVTT\n\n00:00.000 --> 00:05.000\nfirst\n\n00:01.000 --> 00:04.000\nsecond", "second", 200},
		{"badtime.srt", "1\n00:00:99,000 --> 00:00:02,000\nwrong", "分秒", 422},
		{"reverse.vtt", "WEBVTT\n\n00:03.000 --> 00:01.000\nwrong", "起止", 422},
		{"order.vtt", "WEBVTT\n\n00:03.000 --> 00:04.000\na\n\n00:01.000 --> 00:02.000\nb", "顺序", 422},
		{"encoding.srt", string([]byte{0xff, 0xfe, 0x31, 0, 0x32, 0}), "UTF-8", 422},
		{"nul.vtt", "WEBVTT\n\n00:00.000 --> 00:01.000\n\x00", "UTF-8", 422},
		{"header.vtt", "00:00.000 --> 00:01.000\nno header", "WEBVTT", 422},
		{"separator.srt", "1\n00:00:00,000 --> 00:00:01,000\na\n2\n00:00:02,000 --> 00:00:03,000\nb", "结构", 422},
		{"empty.srt", "", "没有可用", 422},
		{"body.srt", "1\n00:00:00,000 --> 00:00:01,000", "正文", 422},
		{"settings.vtt", "WEBVTT\n\n00:00.000 --> 00:01.000 position:101%\na", "位置", 422},
		{"style.vtt", "WEBVTT\n\nSTYLE\n::cue {color:red}\n\n00:00.000 --> 00:01.000\na", "STYLE/REGION", 422},
		{"huge.srt", strings.Repeat("a", (2<<20)+1), "2 MiB", 422},
	}
	for _, s := range samples {
		writeMedia(t, filepath.Join(root, "电影 & #."+s.name), s.text)
	}
	writeMedia(t, filepath.Join(root, "电影 & #extra.vtt"), "not matching")
	writeMedia(t, filepath.Join(root, "电影 & #.zh.forced.vtt"), "outside documented naming")
	writeMedia(t, filepath.Join(root, "电影 & #.ass"), "unsupported")
	if err := os.Symlink(filepath.Join(root, "电影 & #.en.srt"), filepath.Join(root, "电影 & #.link.srt")); err != nil {
		t.Fatal(err)
	}
	a := start(t, app.Config{DataDir: t.TempDir(), Username: "family", Password: password, MediaRoots: []string{root}})
	login(t, a)
	d := addDirectory(t, a, root)
	awaitScan(t, a, d.ID, "completed")
	id := library(t, a)[0].ID
	items := subtitles(t, a, id)
	if len(items) != len(samples) {
		t.Fatalf("wrong candidates: %+v", items)
	}
	for _, s := range samples {
		t.Run(s.name, func(t *testing.T) {
			var selected subtitleItem
			for _, item := range items {
				if item.Filename == "电影 & #."+s.name {
					selected = item
				}
			}
			body, response := request(t, a.client, "GET", a.server.URL+"/api/media/"+id+"/subtitles/"+selected.ID, nil, s.status)
			if !bytes.Contains(body, []byte(s.want)) {
				t.Fatalf("wrong conversion/error: %s", body)
			}
			if s.status == 200 && (!strings.HasPrefix(string(body), "WEBVTT\n\n") || response.Header.Get("Content-Type") != "text/vtt; charset=utf-8") {
				t.Fatal("not actual VTT")
			}
			current, err := os.ReadFile(filepath.Join(root, selected.Filename))
			if err != nil || string(current) != s.text {
				t.Fatal("subtitle source changed")
			}
		})
	}
	content, _ := os.ReadFile(filepath.Join(root, "电影 & #.webm"))
	if string(content) != "video remains unchanged" {
		t.Fatal("source video changed")
	}
}
func TestSubtitlesAreSessionOnlyBoundToMediaAndRejectChanges(t *testing.T) {
	root := t.TempDir()
	writeMedia(t, filepath.Join(root, "one.webm"), "one")
	writeMedia(t, filepath.Join(root, "two.webm"), "two")
	path := filepath.Join(root, "one.srt")
	writeMedia(t, path, "1\n00:00:00,000 --> 00:00:01,000\nHello")
	a := start(t, app.Config{DataDir: t.TempDir(), Username: "family", Password: password, MediaRoots: []string{root}})
	login(t, a)
	d := addDirectory(t, a, root)
	awaitScan(t, a, d.ID, "completed")
	files := library(t, a)
	id, other := files[0].ID, files[1].ID
	items := subtitles(t, a, id)
	if len(items) != 1 {
		t.Fatal("missing same-basename subtitle")
	}
	if len(subtitles(t, a, other)) != 0 {
		t.Fatal("unrelated video has subtitles")
	}
	endpoint := "/api/media/" + id + "/subtitles/" + items[0].ID
	request(t, http.DefaultClient, "GET", a.server.URL+endpoint, nil, 401)
	request(t, http.DefaultClient, "GET", a.server.URL+"/api/media/"+id+"/subtitles", nil, 401)
	link := streamPath(t, a, id)
	token := strings.Split(link, "?token=")[1]
	request(t, http.DefaultClient, "GET", a.server.URL+endpoint+"?token="+token, nil, 401)
	request(t, a.client, "GET", a.server.URL+"/api/media/"+other+"/subtitles/"+items[0].ID, nil, 404)
	request(t, a.client, "GET", a.server.URL+"/api/media/"+id+"/subtitles/not-a-path", nil, 404)
	if os.Geteuid() != 0 {
		if err := os.Chmod(path, 0); err != nil {
			t.Fatal(err)
		}
		request(t, a.client, "GET", a.server.URL+endpoint, nil, 409)
		if err := os.Chmod(path, 0644); err != nil {
			t.Fatal(err)
		}
	}
	writeMedia(t, path, "1\n00:00:00,000 --> 00:00:01,000\nChanged")
	request(t, a.client, "GET", a.server.URL+endpoint, nil, 404)
	next := subtitles(t, a, id)[0]
	if next.ID == items[0].ID {
		t.Fatal("changed subtitle retained identity")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "secret.srt")
	writeMedia(t, target, "secret")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	request(t, a.client, "GET", a.server.URL+"/api/media/"+id+"/subtitles/"+next.ID, nil, 404)
	if len(subtitles(t, a, id)) != 0 {
		t.Fatal("symlink exposed")
	}
	if err := os.Remove(filepath.Join(root, "one.webm")); err != nil {
		t.Fatal(err)
	}
	request(t, a.client, "GET", a.server.URL+"/api/media/"+id+"/subtitles", nil, 410)
}
