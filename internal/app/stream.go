package app

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
)

func (a *App) initializeStreams() error {
	_, err := a.db.Exec(`CREATE TABLE IF NOT EXISTS stream_key (id INTEGER PRIMARY KEY CHECK(id=1), secret BLOB NOT NULL);`)
	if err != nil {
		return err
	}
	secret := make([]byte, 32)
	_, _ = rand.Read(secret)
	if _, err = a.db.Exec("INSERT OR IGNORE INTO stream_key(id,secret) VALUES(1,?)", secret); err != nil {
		return err
	}
	if err = a.db.QueryRow("SELECT secret FROM stream_key WHERE id=1").Scan(&a.streamKey); err != nil {
		return err
	}
	if len(a.streamKey) != 32 {
		return errors.New("串流授权密钥无效")
	}
	return nil
}
func (a *App) streamToken(id string) []byte {
	mac := hmac.New(sha256.New, a.streamKey)
	_, _ = mac.Write([]byte("reelnest:read-media:v1:" + id))
	return mac.Sum(nil)
}
func (a *App) streamLink(w http.ResponseWriter, r *http.Request) {
	var input struct{}
	if !decode(w, r, &input) {
		return
	}
	root, file, err := a.openMedia(r.Context(), r.PathValue("id"))
	if err != nil {
		reportMediaProblem(w, err)
		return
	}
	root.Close()
	file.Close()
	// The browser resolves this against its own origin. Never trust the HTTP Host
	// or forwarding headers to insert an attacker-selected address into a link.
	path := "/stream/" + r.PathValue("id") + "?token=" + hex.EncodeToString(a.streamToken(r.PathValue("id")))
	respond(w, 200, map[string]string{"path": path})
}
func (a *App) stream(w http.ResponseWriter, r *http.Request) {
	tokens := r.URL.Query()["token"]
	if len(tokens) != 1 || len(tokens[0]) != 64 {
		fail(w, 401, "串流凭证无效")
		return
	}
	token, err := hex.DecodeString(tokens[0])
	if err != nil || !hmac.Equal(token, a.streamToken(r.PathValue("id"))) {
		fail(w, 401, "串流凭证无效")
		return
	}
	a.content(w, r)
}
