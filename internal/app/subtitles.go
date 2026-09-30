package app

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unicode/utf8"
)

const subtitleLimit = 2 << 20

var subtitleLanguage = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type subtitle struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	Format      string `json:"format"`
	fingerprint string
}

func matchingSubtitle(video, name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".srt" && ext != ".vtt" {
		return false
	}
	base := strings.TrimSuffix(video, filepath.Ext(video))
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	if stem == base {
		return true
	}
	return strings.HasPrefix(stem, base+".") && subtitleLanguage.MatchString(strings.TrimPrefix(stem, base+"."))
}
func listSubtitles(root *os.Root, video, id string) ([]subtitle, error) {
	dir, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	result := []subtitle{}
	for _, entry := range entries {
		if !matchingSubtitle(video, entry.Name()) {
			continue
		}
		info, err := root.Lstat(entry.Name())
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			continue
		} // Includes rejecting all symlinks.
		fingerprint := fileFingerprint(info)
		hash := sha256.Sum256([]byte(id + "\x00" + entry.Name() + "\x00" + fingerprint))
		result = append(result, subtitle{hex.EncodeToString(hash[:]), entry.Name(), strings.TrimPrefix(strings.ToLower(filepath.Ext(entry.Name())), "."), fingerprint})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Filename < result[j].Filename })
	return result, nil
}
func (a *App) subtitles(w http.ResponseWriter, r *http.Request) {
	root, video, err := a.openMedia(r.Context(), r.PathValue("id"))
	if err != nil {
		reportMediaProblem(w, err)
		return
	}
	defer root.Close()
	defer video.Close()
	candidates, err := listSubtitles(root, filepath.Base(video.Name()), r.PathValue("id"))
	if err != nil {
		fail(w, 409, "无法读取外挂字幕目录，请检查权限")
		return
	}
	if r.PathValue("subtitle") == "" {
		respond(w, 200, map[string]any{"items": candidates})
		return
	}
	var selected *subtitle
	for i := range candidates {
		if candidates[i].ID == r.PathValue("subtitle") {
			selected = &candidates[i]
			break
		}
	}
	if selected == nil {
		fail(w, 404, "字幕不存在、已变化或不属于此媒体文件，请重新打开播放器")
		return
	}
	file, err := root.OpenFile(selected.Filename, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		fail(w, 409, "字幕无法读取，请检查权限或符号链接")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || fileFingerprint(info) != selected.fingerprint {
		fail(w, 409, "字幕文件在读取时发生变化")
		return
	}
	if info.Size() > subtitleLimit {
		fail(w, 422, "字幕超过 2 MiB 限制，视频仍可播放")
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, subtitleLimit+1))
	if err != nil {
		fail(w, 409, "字幕读取失败，视频仍可播放")
		return
	}
	after, err := file.Stat()
	if err != nil || fileFingerprint(after) != selected.fingerprint {
		fail(w, 409, "字幕文件在读取时发生变化")
		return
	}
	converted, err := subtitleVTT(data, selected.Format)
	if err != nil {
		fail(w, 422, "字幕不可用："+err.Error()+"；视频仍可播放")
		return
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	_, _ = io.WriteString(w, converted)
}

var cueTiming = regexp.MustCompile(`^([^\s]+)[ \t]+-->[ \t]+([^\s]+)(?:[ \t]+(.*))?$`)
var srtTimestamp = regexp.MustCompile(`^(\d{2,}):(\d{2}):(\d{2}),(\d{3})$`)
var vttTimestamp = regexp.MustCompile(`^(?:(\d{2,}):)?(\d{2}):(\d{2})\.(\d{3})$`)
var srtIndex = regexp.MustCompile(`^\d+$`)

func timestamp(text, format string) (int64, string, error) {
	pattern := vttTimestamp
	if format == "srt" {
		pattern = srtTimestamp
	}
	parts := pattern.FindStringSubmatch(text)
	if parts == nil {
		return 0, "", fmt.Errorf("时间戳格式错误")
	}
	hours := 0
	var err error
	if parts[1] != "" {
		hours, err = strconv.Atoi(parts[1])
		if err != nil || hours > 99999 {
			return 0, "", fmt.Errorf("时间戳小时数无效")
		}
	}
	minutes, _ := strconv.Atoi(parts[2])
	seconds, _ := strconv.Atoi(parts[3])
	millis, _ := strconv.Atoi(parts[4])
	if minutes > 59 || seconds > 59 {
		return 0, "", fmt.Errorf("时间戳分秒必须小于 60")
	}
	return (int64(hours)*3600+int64(minutes)*60+int64(seconds))*1000 + int64(millis), fmt.Sprintf("%02d:%02d:%02d.%03d", hours, minutes, seconds, millis), nil
}

var percentageSyntax = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?%$`)
var lineNumberSyntax = regexp.MustCompile(`^-?[0-9]+$`)

func percent(value string) bool {
	if !percentageSyntax.MatchString(value) {
		return false
	}
	n, err := strconv.ParseFloat(strings.TrimSuffix(value, "%"), 64)
	return err == nil && n >= 0 && n <= 100
}
func cueSettings(text string) bool {
	seen := map[string]bool{}
	for _, setting := range strings.Fields(text) {
		pair := strings.SplitN(setting, ":", 2)
		if len(pair) != 2 || seen[pair[0]] {
			return false
		}
		seen[pair[0]] = true
		value := pair[1]
		switch pair[0] {
		case "align":
			if value != "start" && value != "center" && value != "end" && value != "left" && value != "right" {
				return false
			}
		case "vertical":
			if value != "rl" && value != "lr" {
				return false
			}
		case "size":
			if !percent(value) {
				return false
			}
		case "position", "line":
			parts := strings.Split(value, ",")
			if len(parts) > 2 {
				return false
			}
			if pair[0] == "position" {
				if !percent(parts[0]) {
					return false
				}
			} else {
				_, err := strconv.Atoi(parts[0])
				if parts[0] != "auto" && (err != nil || !lineNumberSyntax.MatchString(parts[0])) && !percent(parts[0]) {
					return false
				}
			}
			if len(parts) == 2 {
				valid := parts[1] == "center"
				if pair[0] == "line" {
					valid = valid || parts[1] == "start" || parts[1] == "end"
				} else {
					valid = valid || parts[1] == "line-left" || parts[1] == "line-right" || parts[1] == "auto"
				}
				if !valid {
					return false
				}
			}
		default:
			return false
		}
	}
	return true
}
func subtitleVTT(data []byte, format string) (string, error) {
	if len(data) > subtitleLimit {
		return "", fmt.Errorf("字幕超过 2 MiB 限制")
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("仅支持 UTF-8 编码")
	}
	text := strings.TrimPrefix(string(data), "\ufeff")
	if strings.ContainsRune(text, 0) {
		return "", fmt.Errorf("包含无效文本字符，仅支持 UTF-8")
	}
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(text, "\n")
	if format == "vtt" {
		if len(lines) == 0 || (lines[0] != "WEBVTT" && !strings.HasPrefix(lines[0], "WEBVTT ") && !strings.HasPrefix(lines[0], "WEBVTT\t")) || strings.Contains(lines[0], "-->") {
			return "", fmt.Errorf("缺少有效 WEBVTT 文件头")
		}
		if len(lines) < 2 || strings.TrimSpace(lines[1]) != "" {
			return "", fmt.Errorf("WEBVTT 文件头后必须为空行")
		}
		lines = lines[2:]
	}
	var out strings.Builder
	out.WriteString("WEBVTT\n\n")
	count := 0
	previous := int64(-1)
	for i := 0; i < len(lines); {
		if strings.TrimSpace(lines[i]) == "" {
			i++
			continue
		}
		start := i
		for i < len(lines) && strings.TrimSpace(lines[i]) != "" {
			i++
		}
		block := lines[start:i]
		if format == "vtt" && (block[0] == "NOTE" || strings.HasPrefix(block[0], "NOTE ") || strings.HasPrefix(block[0], "NOTE\t")) {
			continue
		}
		if block[0] == "STYLE" || block[0] == "REGION" {
			return "", fmt.Errorf("暂不支持 VTT STYLE/REGION 样式块")
		}
		line := 0
		if !strings.Contains(block[0], "-->") {
			if format == "srt" && !srtIndex.MatchString(block[0]) {
				return "", fmt.Errorf("SRT 序号或结构错误")
			}
			line = 1
		}
		if len(block) <= line+1 {
			return "", fmt.Errorf("缺少字幕时间或正文")
		}
		timing := cueTiming.FindStringSubmatch(block[line])
		if timing == nil {
			return "", fmt.Errorf("字幕时间行格式错误")
		}
		begin, beginText, err := timestamp(timing[1], format)
		if err != nil {
			return "", err
		}
		end, endText, err := timestamp(timing[2], format)
		if err != nil {
			return "", err
		}
		if end <= begin || begin < previous {
			return "", fmt.Errorf("字幕起止时间或排列顺序错误")
		}
		previous = begin
		if (format == "srt" && timing[3] != "") || !cueSettings(timing[3]) {
			return "", fmt.Errorf("不支持或无效的字幕位置设置")
		}
		body := strings.Join(block[line+1:], "\n")
		if strings.Contains(body, "-->") {
			return "", fmt.Errorf("字幕结构错误，字幕块之间需要空行")
		}
		fmt.Fprintf(&out, "%s --> %s", beginText, endText)
		if timing[3] != "" {
			out.WriteByte(' ')
			out.WriteString(timing[3])
		}
		out.WriteByte('\n')
		out.WriteString(body)
		out.WriteString("\n\n")
		count++
	}
	if count == 0 {
		return "", fmt.Errorf("没有可用字幕内容")
	}
	return out.String(), nil
}
