package app

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const cookieName = "reelnest_session"
const sessionLifetime = 30 * 24 * time.Hour

func tokenHash(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func (a *App) authorized(r *http.Request) bool {
	cookie, err := r.Cookie(cookieName)
	if err != nil || len(cookie.Value) != 64 {
		return false
	}
	var expires int64
	err = a.db.QueryRowContext(r.Context(), "SELECT expires_at FROM sessions WHERE token_hash=?", tokenHash(cookie.Value)).Scan(&expires)
	return err == nil && expires > time.Now().Unix()
}

func (a *App) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.authorized(r) {
			fail(w, 401, "请先使用家庭共享账号登录")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &input) {
		return
	}
	// A bounded, shared limiter is appropriate for the single household account.
	// Serialize password verification so an unauthenticated caller cannot exhaust CPU.
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	now := time.Now()
	if now.Unix()-a.loginWindow >= 300 {
		a.loginWindow = now.Unix()
		a.failedLogins = 0
	}
	if a.failedLogins >= 10 {
		w.Header().Set("Retry-After", "300")
		fail(w, 429, "登录失败次数过多，请稍后重试")
		return
	}
	var username string
	var hash []byte
	if err := a.db.QueryRowContext(r.Context(), "SELECT username, password_hash FROM household WHERE id=1").Scan(&username, &hash); err != nil {
		fail(w, 500, "无法读取家庭共享账号")
		return
	}
	validPassword := bcrypt.CompareHashAndPassword(hash, []byte(input.Password)) == nil
	if username != input.Username || !validPassword {
		a.failedLogins++
		fail(w, 401, "账号或密码错误")
		return
	}
	a.failedLogins = 0
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		fail(w, 500, "无法建立会话")
		return
	}
	token := hex.EncodeToString(tokenBytes)
	expiry := now.Add(sessionLifetime)
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "无法建立会话")
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(r.Context(), "DELETE FROM sessions WHERE expires_at<=?", now.Unix())
	if err == nil {
		_, err = tx.ExecContext(r.Context(), "INSERT INTO sessions(token_hash, expires_at) VALUES (?, ?)", tokenHash(token), expiry.Unix())
	}
	if old, e := r.Cookie(cookieName); e == nil && err == nil {
		_, err = tx.ExecContext(r.Context(), "DELETE FROM sessions WHERE token_hash=?", tokenHash(old.Value))
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		fail(w, 500, "无法建立会话")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true, Secure: a.config.CookieSecure, SameSite: http.SameSiteStrictMode, MaxAge: int(sessionLifetime.Seconds()), Expires: expiry})
	respond(w, 200, map[string]string{"username": username})
}

func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	var input struct{}
	if !decode(w, r, &input) {
		return
	}
	cookie, _ := r.Cookie(cookieName)
	if _, err := a.db.ExecContext(r.Context(), "DELETE FROM sessions WHERE token_hash=?", tokenHash(cookie.Value)); err != nil {
		fail(w, 500, "退出失败，请重试")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", HttpOnly: true, Secure: a.config.CookieSecure, SameSite: http.SameSiteStrictMode, MaxAge: -1, Expires: time.Unix(1, 0)})
	respond(w, 200, map[string]bool{"ok": true})
}

func (a *App) session(w http.ResponseWriter, r *http.Request) {
	var username string
	if err := a.db.QueryRowContext(r.Context(), "SELECT username FROM household WHERE id=1").Scan(&username); err != nil {
		fail(w, 500, "无法读取家庭共享账号")
		return
	}
	respond(w, 200, map[string]string{"username": username})
}
