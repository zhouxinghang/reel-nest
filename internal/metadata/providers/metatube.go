package providers

import (
	"context"
	"encoding/json"
	"net/url"
	"reelnest/internal/model"
	"strings"
)

type metaMovie struct {
	ID          string   `json:"id"`
	Provider    string   `json:"provider"`
	Number      string   `json:"number"`
	Title       string   `json:"title"`
	Summary     string   `json:"summary"`
	Homepage    string   `json:"homepage"`
	Director    string   `json:"director"`
	Actors      []string `json:"actors"`
	ReleaseDate string   `json:"release_date"`
}

func (c *client) metaSearch(ctx context.Context, q string) (model.SearchResult, error) {
	var r struct {
		Data  []metaMovie     `json:"data"`
		Error json.RawMessage `json:"error"`
	}
	status, e := c.json(ctx, "/movies/search?"+url.Values{"q": {q}, "fallback": {"true"}}.Encode(), &r)
	result := model.SearchResult{Items: []model.Candidate{}, Complete: true}
	if status == 404 {
		return result, nil
	}
	if e != nil {
		return result, e
	}
	if r.Data == nil {
		return result, model.ErrProvider
	}
	if len(r.Error) > 0 && string(r.Error) != "null" && string(r.Error) != "\"\"" {
		return result, model.ErrProvider
	}
	for _, x := range r.Data {
		addCandidate(&result, model.Candidate{Ref: c.ref("movie", x.ID, strings.ToLower(x.Provider)), Title: x.Title, OriginalTitle: x.Title, Identifier: x.Number, Overview: x.Summary, Year: year(x.ReleaseDate)})
	}
	return result, nil
}
func (c *client) metaTitle(ctx context.Context, r model.RemoteRef, p Parts) (model.Title, error) {
	var reply struct {
		Data  metaMovie       `json:"data"`
		Error json.RawMessage `json:"error"`
	}
	_, e := c.json(ctx, "/movies/"+url.PathEscape(r.Namespace)+"/"+url.PathEscape(r.RemoteID)+"?lazy=true", &reply)
	t := model.Title{CreditsState: "ready", ArtworkState: "ready", Artwork: map[string]string{}}
	x := reply.Data
	if e != nil {
		return t, e
	}
	if len(reply.Error) > 0 && string(reply.Error) != "null" && string(reply.Error) != "\"\"" {
		return t, model.ErrProvider
	}
	if x.ID != r.RemoteID || strings.ToLower(x.Provider) != r.Namespace {
		return t, model.ErrMetadata
	}
	t.Title = x.Title
	t.OriginalTitle = x.Title
	t.Identifier = x.Number
	t.Overview = x.Summary
	t.SourceURL = x.Homepage
	t.Year = year(x.ReleaseDate)
	if p.Credits {
		for _, name := range x.Actors {
			addCredit(&t, model.Credit{Name: name, Role: "actor"})
		}
		if x.Director != "" {
			addCredit(&t, model.Credit{Name: x.Director, Role: "director"})
		}
	}
	if p.Artwork || p.Basic {
		ref, _ := json.Marshal(r)
		for _, kind := range []string{"primary", "thumb", "backdrop"} {
			t.Artwork[kind] = string(ref)
		}
	}
	return t, nil
}
