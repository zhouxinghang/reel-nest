package http

import (
	"encoding/json"
	"net/http"

	"reelnest/internal/metadata"
	"reelnest/internal/model"
)

type providerSettingsDTO struct {
	Source        string  `json:"source"`
	Name          string  `json:"name"`
	URL           *string `json:"url"`
	CatalogID     *string `json:"catalogId"`
	CredentialSet bool    `json:"credentialSet"`
	PINSet        bool    `json:"pinSet"`
	Configured    bool    `json:"configured"`
	Revision      int64   `json:"revision"`
}

func providerName(source string) string {
	return map[string]string{"tmdb": "TMDB", "tvdb": "TVDB", "metatube": "MetaTube"}[source]
}
func providerSettings(p metadata.ProviderSnapshot) providerSettingsDTO {
	result := providerSettingsDTO{Source: p.Source(), Name: providerName(p.Source()), CredentialSet: p.Credential() != "", PINSet: p.PIN() != "", Configured: p.Configured(), Revision: p.Revision()}
	if id := p.CatalogID(); id != "" {
		result.CatalogID = &id
	}
	if base := p.BaseURL(); p.Source() == "metatube" && base != "" {
		result.URL = &base
	}
	return result
}
func (t *Transport) metadataSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := t.metadata.Settings()
	if err != nil {
		t.fail(w, err)
		return
	}
	result := struct {
		Sources []providerSettingsDTO `json:"sources"`
	}{Sources: []providerSettingsDTO{}}
	for _, p := range settings {
		result.Sources = append(result.Sources, providerSettings(p))
	}
	respond(w, 200, result)
}
func (t *Transport) updateMetadataSettings(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	var input struct {
		Revision        int64   `json:"revision"`
		Credential      *string `json:"credential"`
		PIN             *string `json:"pin"`
		URL             *string `json:"url"`
		ClearCredential bool    `json:"clearCredential"`
		ClearPIN        bool    `json:"clearPin"`
	}
	if json.NewDecoder(r.Body).Decode(&raw) != nil || json.Unmarshal(raw, &input) != nil {
		t.fail(w, model.ErrInvalid)
		return
	}
	// Reject source-inapplicable fields even when their values are null/false.
	// Shape, unknown fields, size and trailing JSON are checked by the contract.
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		t.fail(w, model.ErrInvalid)
		return
	}
	source := r.PathValue("source")
	if (source != "tvdb" && (fields["pin"] != nil || fields["clearPin"] != nil)) || (source != "metatube" && fields["url"] != nil) {
		t.fail(w, model.ErrInvalid)
		return
	}
	p, err := t.metadata.Update(r.Context(), source, metadata.SettingsPatch{
		Revision: input.Revision, Credential: input.Credential, PIN: input.PIN, URL: input.URL,
		ClearCredential: input.ClearCredential, ClearPIN: input.ClearPIN,
	})
	if err != nil {
		t.fail(w, err)
		return
	}
	respond(w, 200, providerSettings(p))
}
