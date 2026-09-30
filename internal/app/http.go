package app

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
)

func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func fail(w http.ResponseWriter, status int, message string) {
	respond(w, status, map[string]string{"error": message})
}

func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType != "application/json" {
		fail(w, 415, "请求必须使用 application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		fail(w, 400, "请求内容无效")
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		fail(w, 400, "请求内容无效")
		return false
	}
	return true
}

func (a *App) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; media-src 'self' blob:; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'")
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				fail(w, 403, "拒绝跨站请求")
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				parsed, err := url.Parse(origin)
				scheme := "http"
				if r.TLS != nil || a.config.CookieSecure {
					scheme = "https"
				}
				if err != nil || parsed.Scheme != scheme || parsed.Host != r.Host || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
					fail(w, 403, "拒绝跨站请求")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) page(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method not allowed", 405)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/assets/") && a.config.Assets != nil {
		http.FileServer(http.FS(a.config.Assets)).ServeHTTP(w, r)
		return
	}
	if r.URL.Path != "/" && r.URL.Path != "/login" && r.URL.Path != "/library" && r.URL.Path != "/directories" && !strings.HasPrefix(r.URL.Path, "/media/") && !strings.HasPrefix(r.URL.Path, "/series/") {
		http.NotFound(w, r)
		return
	}
	if r.URL.Path != "/login" && !a.authorized(r) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if a.config.Assets == nil {
		http.Error(w, "前端尚未构建", 503)
		return
	}
	contents, err := a.config.Assets.Open("index.html")
	if err != nil {
		http.Error(w, "前端尚未构建", 503)
		return
	}
	defer contents.Close()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.Copy(w, contents)
}
