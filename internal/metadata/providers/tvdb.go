package providers

import (
	"context"
	"encoding/json"
	"net/url"
	"reelnest/internal/model"
	"sort"
	"strings"
)

type tvdbSearchRecord struct {
	ID           string            `json:"tvdb_id"`
	Type         string            `json:"type"`
	Name         string            `json:"name"`
	Year         string            `json:"year"`
	Overview     string            `json:"overview"`
	Translations map[string]string `json:"translations"`
	Overviews    map[string]string `json:"overviews"`
}

func (c *client) tvdbSearch(ctx context.Context, kind, q string) (model.SearchResult, error) {
	var r struct {
		Status string             `json:"status"`
		Data   []tvdbSearchRecord `json:"data"`
		Links  struct {
			Next json.RawMessage `json:"next"`
		} `json:"links"`
	}
	_, e := c.json(ctx, "/search?"+url.Values{"query": {q}, "type": {kind}, "limit": {"21"}, "offset": {"0"}}.Encode(), &r)
	result := model.SearchResult{Items: []model.Candidate{}, Complete: r.Data != nil && len(r.Data) < 21 && (len(r.Links.Next) == 0 || string(r.Links.Next) == "null" || string(r.Links.Next) == "\"\"")}
	if e != nil {
		return result, e
	}
	if r.Status != "success" {
		return result, model.ErrProvider
	}
	for _, x := range r.Data {
		if x.Type != kind {
			result.Complete = false
			continue
		}
		addCandidate(&result, model.Candidate{Ref: c.ref(kind, x.ID, "tvdb"), Title: first(x.Translations["zho"], x.Translations["eng"], x.Name), OriginalTitle: x.Name, Overview: first(x.Overviews["zho"], x.Overviews["eng"], x.Overview), Year: year(x.Year)})
	}
	return result, nil
}

type tvdbTitle struct {
	ID           json.Number `json:"id"`
	Name         string      `json:"name"`
	Overview     string      `json:"overview"`
	Year         string      `json:"year"`
	FirstAired   string      `json:"firstAired"`
	Slug         string      `json:"slug"`
	Image        string      `json:"image"`
	Translations struct {
		Name []struct {
			Language string `json:"language"`
			Name     string `json:"name"`
		} `json:"nameTranslations"`
		Overview []struct {
			Language string `json:"language"`
			Overview string `json:"overview"`
		} `json:"overviewTranslations"`
	} `json:"translations"`
	Characters []struct {
		PersonID  json.Number `json:"peopleId"`
		Name      string      `json:"personName"`
		Image     string      `json:"personImgURL"`
		Character string      `json:"name"`
		Type      string      `json:"peopleType"`
		Sort      int         `json:"sort"`
	} `json:"characters"`
	Artworks []struct {
		Image    string  `json:"image"`
		Type     int     `json:"type"`
		Language string  `json:"language"`
		Width    int     `json:"width"`
		Height   int     `json:"height"`
		Score    float64 `json:"score"`
	} `json:"artworks"`
}

func (c *client) tvdbTitle(ctx context.Context, r model.RemoteRef, p Parts) (model.Title, error) {
	t := model.Title{Artwork: map[string]string{}, CreditsState: "missing", ArtworkState: "missing"}
	resource := "movies"
	if r.Kind == "series" {
		resource = "series"
	}
	short := "true"
	if !p.Basic && (p.Credits || p.Artwork) {
		short = "false"
	}
	var reply struct {
		Status string    `json:"status"`
		Data   tvdbTitle `json:"data"`
	}
	_, e := c.json(ctx, "/"+resource+"/"+r.RemoteID+"/extended?meta=translations&short="+short, &reply)
	if e != nil {
		return t, e
	}
	x := reply.Data
	if reply.Status != "success" || string(x.ID) != r.RemoteID {
		return t, model.ErrMetadata
	}
	t.Title = x.Name
	t.OriginalTitle = x.Name
	t.Overview = x.Overview
	t.Year = year(first(x.Year, x.FirstAired))
	t.SourceURL = "https://thetvdb.com/" + resource + "/" + url.PathEscape(first(x.Slug, r.RemoteID))
	for _, language := range []string{"eng", "zho"} {
		for _, v := range x.Translations.Name {
			if v.Language == language && v.Name != "" {
				t.Title = v.Name
			}
		}
		for _, v := range x.Translations.Overview {
			if v.Language == language && v.Overview != "" {
				t.Overview = v.Overview
			}
		}
	}
	if v := tvdbImage(x.Image); v != "" {
		t.Artwork["primary"] = v
	}
	// Ask for the optional expansion only when the basic response did not
	// include it. Failure here must not discard an already verified title.
	if p.Basic && ((p.Credits && x.Characters == nil) || (p.Artwork && x.Artworks == nil)) {
		var expanded struct {
			Status string    `json:"status"`
			Data   tvdbTitle `json:"data"`
		}
		_, e = c.json(ctx, "/"+resource+"/"+r.RemoteID+"/extended?meta=translations&short=false", &expanded)
		if e != nil || expanded.Status != "success" || string(expanded.Data.ID) != r.RemoteID {
			if p.Credits {
				t.CreditsState = "failed"
			}
			if p.Artwork {
				t.ArtworkState = "failed"
			}
			return t, nil
		}
		x.Characters = expanded.Data.Characters
		x.Artworks = expanded.Data.Artworks
	}
	if p.Credits {
		t.CreditsState = "ready"
		if x.Characters == nil {
			t.CreditsState = "failed"
		}
		sort.SliceStable(x.Characters, func(i, j int) bool { return x.Characters[i].Sort < x.Characters[j].Sort })
		for _, v := range x.Characters {
			role := strings.ToLower(v.Type)
			if role != "actor" && role != "director" {
				continue
			}
			key := string(v.PersonID)
			if !digits.MatchString(key) {
				key = ""
			}
			chars := []string{}
			if role == "actor" && v.Character != "" {
				chars = append(chars, clip(v.Character, 512))
			}
			addCredit(&t, model.Credit{Key: key, Name: v.Name, Role: role, Photo: tvdbImage(v.Image), Characters: chars})
		}
	}
	if p.Artwork {
		var types struct {
			Status string `json:"status"`
			Data   []struct {
				ID         int    `json:"id"`
				Name       string `json:"name"`
				Slug       string `json:"slug"`
				RecordType string `json:"recordType"`
			} `json:"data"`
		}
		_, e := c.json(ctx, "/artwork/types", &types)
		if e == nil && (types.Status != "success" || types.Data == nil || x.Artworks == nil) {
			e = model.ErrProvider
		}
		t.ArtworkState = optional(e)
		if e == nil {
			allowed := map[int]bool{}
			for _, v := range types.Data {
				n := strings.ToLower(v.Name + " " + v.Slug)
				if v.RecordType == r.Kind && (strings.Contains(n, "background") || strings.Contains(n, "fanart") || strings.Contains(n, "backdrop")) {
					allowed[v.ID] = true
				}
			}
			for _, kind := range []string{"thumb", "backdrop"} {
				best := -1.0
				for _, v := range x.Artworks {
					if !allowed[v.Type] || v.Height == 0 || float64(v.Width)/float64(v.Height) < 1.4 || float64(v.Width)/float64(v.Height) > 2 || tvdbImage(v.Image) == "" {
						continue
					}
					score := v.Score
					if (kind == "thumb" && v.Language == "zho") || (kind == "backdrop" && (v.Language == "" || v.Language == "und")) {
						score += 100
					}
					if score > best {
						best = score
						t.Artwork[kind] = v.Image
					}
				}
			}
		}
	}
	return t, nil
}
