package app

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// LegacyMetadataCredentials is imported once, only when no saved settings exist.
// It is never returned by an HTTP endpoint or retained in application Config.
type LegacyMetadataCredentials struct {
	TMDBToken, TVDBAPIKey, TVDBPIN, MetaTubeURL, MetaTubeToken string
}

type providerSettings struct {
	Source     string
	URL        string
	Credential string
	PIN        string
	Revision   int64
}

type providerSettingsView struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	URL           string `json:"url"`
	CredentialSet bool   `json:"credentialSet"`
	PINSet        bool   `json:"pinSet"`
	Configured    bool   `json:"configured"`
	Revision      int64  `json:"revision"`
}

func (s providerSettings) view() providerSettingsView {
	name := ""
	for _, source := range metadataSources {
		if source.id == s.Source {
			name = source.name
		}
	}
	configured := s.Credential != ""
	if s.Source == "metatube" {
		configured = s.URL != ""
	}
	return providerSettingsView{s.Source, name, s.URL, s.Credential != "", s.PIN != "", configured, s.Revision}
}

// A separate 0600 key keeps credentials out of SQLite, WAL, SQL diagnostics and
// database-only exports. Full data-directory backups still contain both secrets
// and their key and must be protected as sensitive data.
func (a *App) settingsCipher(create bool) (cipher.AEAD, error) {
	path := filepath.Join(a.config.DataDir, "metadata-settings.key")
	key, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) && create {
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, errors.New("无法生成资料源配置密钥")
		}
		var file *os.File
		file, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err == nil {
			_, err = file.Write(key)
			if err == nil {
				err = file.Sync()
			}
			closeErr := file.Close()
			if err == nil {
				err = closeErr
			}
		}
	}
	if err != nil || len(key) != 32 {
		return nil, errors.New("无法读取资料源配置密钥，请恢复完整应用数据备份")
	}
	if err := os.Chmod(path, 0600); err != nil {
		return nil, errors.New("无法保护资料源配置密钥")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.New("资料源配置密钥无效")
	}
	return cipher.NewGCM(block)
}

func settingsAAD(s providerSettings) []byte {
	return []byte("reelnest-metadata-settings-v1\x00" + s.Source + "\x00" + s.URL)
}

func (a *App) sealSettings(s providerSettings) ([]byte, error) {
	payload, err := json.Marshal(struct{ Credential, PIN string }{s.Credential, s.PIN})
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, a.settingsAEAD.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return a.settingsAEAD.Seal(nonce, nonce, payload, settingsAAD(s)), nil
}

func (a *App) readProviderSettings(ctx context.Context, source string) (providerSettings, error) {
	s := providerSettings{Source: source}
	var sealed []byte
	err := a.db.QueryRowContext(ctx, "SELECT url,secrets,revision FROM metadata_provider_settings WHERE source=?", source).Scan(&s.URL, &sealed, &s.Revision)
	if err != nil {
		return s, err
	}
	size := a.settingsAEAD.NonceSize()
	if len(sealed) < size {
		return s, errors.New("资料源配置无效")
	}
	payload, err := a.settingsAEAD.Open(nil, sealed[:size], sealed[size:], settingsAAD(s))
	if err != nil {
		return s, errors.New("无法解密资料源配置")
	}
	var secrets struct{ Credential, PIN string }
	if err := json.Unmarshal(payload, &secrets); err != nil {
		return s, errors.New("资料源配置无效")
	}
	s.Credential, s.PIN = secrets.Credential, secrets.PIN
	return s, nil
}

func (a *App) newProvider(s providerSettings) (metadataProvider, error) {
	config := a.config // Only test origins/timeouts survive bootstrap; credentials are local.
	switch s.Source {
	case "tmdb":
		config.TMDBToken = s.Credential
		return newTMDB(config)
	case "tvdb":
		config.TVDBAPIKey, config.TVDBPIN = s.Credential, s.PIN
		return newTVDB(config)
	case "metatube":
		config.MetaTubeURL, config.MetaTubeToken = s.URL, s.Credential
		return newMetaTube(config, a.db)
	}
	return nil, errors.New("未知资料源")
}

func validProviderSecret(value string) bool {
	return len(value) <= 4096 && !strings.ContainsFunc(value, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) })
}

func normalizeProviderSettings(s *providerSettings) error {
	s.Credential, s.PIN = strings.TrimSpace(s.Credential), strings.TrimSpace(s.PIN)
	s.URL = strings.TrimRight(strings.TrimSpace(s.URL), "/")
	if !validProviderSecret(s.Credential) || !validProviderSecret(s.PIN) {
		return errors.New("凭据或 PIN 过长，或包含空白及控制字符")
	}
	if len(s.URL) > 2048 {
		return errors.New("MetaTube 服务地址过长")
	}
	return nil
}

func (a *App) initializeMetadataSettings() (map[string]metadataProvider, error) {
	_, err := a.db.Exec(`CREATE TABLE IF NOT EXISTS metadata_provider_settings (
 source TEXT PRIMARY KEY CHECK(source IN ('tmdb','tvdb','metatube')),
 url TEXT NOT NULL, secrets BLOB NOT NULL, revision INTEGER NOT NULL CHECK(revision>0)
 )`)
	if err != nil {
		return nil, err
	}
	var count int
	if err := a.db.QueryRow("SELECT COUNT(*) FROM metadata_provider_settings").Scan(&count); err != nil {
		return nil, err
	}
	a.settingsAEAD, err = a.settingsCipher(count == 0)
	if err != nil {
		return nil, err
	}
	if count == 0 {
		initial := LegacyMetadataCredentials{a.config.TMDBToken, a.config.TVDBAPIKey, a.config.TVDBPIN, a.config.MetaTubeURL, a.config.MetaTubeToken}
		if a.config.ImportLegacyMetadata != nil {
			initial = a.config.ImportLegacyMetadata()
		}
		settings := []providerSettings{
			{Source: "tmdb", Credential: initial.TMDBToken, Revision: 1},
			{Source: "tvdb", Credential: initial.TVDBAPIKey, PIN: initial.TVDBPIN, Revision: 1},
			{Source: "metatube", URL: initial.MetaTubeURL, Credential: initial.MetaTubeToken, Revision: 1},
		}
		// Validate before publishing any settings; a failed migration is retryable.
		for i := range settings {
			if err := normalizeProviderSettings(&settings[i]); err != nil {
				return nil, err
			}
			if _, err := a.newProvider(settings[i]); err != nil {
				return nil, err
			}
		}
		tx, err := a.db.Begin()
		if err != nil {
			return nil, err
		}
		defer tx.Rollback()
		for _, s := range settings {
			sealed, err := a.sealSettings(s)
			if err != nil {
				return nil, err
			}
			if _, err := tx.Exec("INSERT INTO metadata_provider_settings VALUES(?,?,?,?)", s.Source, s.URL, sealed, s.Revision); err != nil {
				return nil, err
			}
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
	}
	providers := map[string]metadataProvider{}
	for _, source := range metadataSources {
		s, err := a.readProviderSettings(a.ctx, source.id)
		if err != nil {
			return nil, errors.New("无法读取已保存的资料源配置，请恢复完整应用数据备份")
		}
		providers[source.id], err = a.newProvider(s)
		if err != nil {
			return nil, err
		}
	}
	return providers, nil
}

func (s *metadataService) provider(source string) metadataProvider {
	s.providersMu.RLock()
	defer s.providersMu.RUnlock()
	return s.providers[source]
}

func (a *App) metadataSettings(w http.ResponseWriter, r *http.Request) {
	result := []providerSettingsView{}
	for _, source := range metadataSources {
		s, err := a.readProviderSettings(r.Context(), source.id)
		if err != nil {
			fail(w, 500, "无法读取资料源配置")
			return
		}
		result = append(result, s.view())
	}
	respond(w, 200, map[string]any{"sources": result})
}

func (a *App) updateMetadataSettings(w http.ResponseWriter, r *http.Request) {
	source := r.PathValue("source")
	if !validMetadataSource(source) {
		fail(w, 404, "资料源不存在")
		return
	}
	var input struct {
		Revision        *int64  `json:"revision"`
		URL             *string `json:"url"`
		Credential      string  `json:"credential"`
		ClearCredential bool    `json:"clearCredential"`
		PIN             string  `json:"pin"`
		ClearPIN        bool    `json:"clearPin"`
	}
	if !decode(w, r, &input) {
		return
	}
	input.Credential, input.PIN = strings.TrimSpace(input.Credential), strings.TrimSpace(input.PIN)
	if input.Revision == nil || *input.Revision <= 0 ||
		(input.ClearCredential && input.Credential != "") || (input.ClearPIN && input.PIN != "") ||
		(source != "metatube" && input.URL != nil) || (source != "tvdb" && (input.PIN != "" || input.ClearPIN)) {
		fail(w, 400, "配置字段无效；替换和清除凭据不能同时选择")
		return
	}
	// Serialize persistence and client replacement with association writes. Searches
	// already underway may finish with their immutable old client; new calls use the
	// saved configuration. No mutable credential is shared with an in-flight request.
	if !a.metadata.operation.TryLock() {
		fail(w, 409, "影片资料获取正在进行，请稍后保存")
		return
	}
	defer a.metadata.operation.Unlock()
	old, err := a.readProviderSettings(r.Context(), source)
	if err != nil {
		fail(w, 500, "无法读取资料源配置")
		return
	}
	if old.Revision != *input.Revision {
		fail(w, 409, "配置已被其他页面修改，请重新加载后再保存")
		return
	}
	next := old
	if input.URL != nil {
		next.URL = *input.URL
	}
	if input.Credential != "" {
		next.Credential = input.Credential
	}
	if input.ClearCredential {
		next.Credential = ""
	}
	if input.PIN != "" {
		next.PIN = input.PIN
	}
	if input.ClearPIN {
		next.PIN = ""
	}
	if err := normalizeProviderSettings(&next); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if next.URL != old.URL && old.Credential != "" && input.Credential == "" && !input.ClearCredential {
		fail(w, 400, "更改 MetaTube 地址时，请重新填写或明确清除 Token，避免将旧凭据发送给新服务")
		return
	}
	client, err := a.newProvider(next)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	if next == old {
		respond(w, 200, old.view())
		return
	}
	sealed, err := a.sealSettings(next)
	if err != nil {
		fail(w, 500, "无法保护资料源配置")
		return
	}
	result, err := a.db.ExecContext(r.Context(), "UPDATE metadata_provider_settings SET url=?,secrets=?,revision=revision+1 WHERE source=? AND revision=?", next.URL, sealed, source, old.Revision)
	if err != nil {
		fail(w, 500, "无法保存资料源配置，原配置未改变")
		return
	}
	if count, err := result.RowsAffected(); err != nil || count != 1 {
		fail(w, 409, "配置已变化，请重新加载后再保存")
		return
	}
	a.metadata.providersMu.Lock()
	a.metadata.providers[source] = client
	a.metadata.providersMu.Unlock()
	a.metadata.mu.Lock()
	a.metadata.revision++
	a.metadata.lastError = ""
	a.metadata.mu.Unlock()
	next.Revision++
	a.queueMetadata()
	respond(w, 200, next.view())
}
