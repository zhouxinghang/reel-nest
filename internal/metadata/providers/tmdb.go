package providers

import (
	"context"
	"encoding/json"
	"net/url"
	"reelnest/internal/model"
	"sort"
)

type tmdbRecord struct {
	ID            json.Number `json:"id"`
	Title         string      `json:"title"`
	Name          string      `json:"name"`
	OriginalTitle string      `json:"original_title"`
	OriginalName  string      `json:"original_name"`
	Overview      string      `json:"overview"`
	ReleaseDate   string      `json:"release_date"`
	FirstAirDate  string      `json:"first_air_date"`
	Poster        string      `json:"poster_path"`
}

func tmdbKind(kind string) string {
	if kind == "series" {
		return "tv"
	}
	return "movie"
}
func (c *client) tmdbSearch(ctx context.Context, kind, q string) (model.SearchResult, error) {
	var r struct {
		Results      []tmdbRecord `json:"results"`
		TotalPages   *int         `json:"total_pages"`
		TotalResults *int         `json:"total_results"`
	}
	_, e := c.json(ctx, "/search/"+tmdbKind(kind)+"?"+url.Values{"query": {q}, "language": {"zh-CN"}, "include_adult": {"false"}, "page": {"1"}}.Encode(), &r)
	result := model.SearchResult{Items: []model.Candidate{}, Complete: r.Results != nil && r.TotalPages != nil && *r.TotalPages >= 0 && *r.TotalPages <= 1 && r.TotalResults != nil && *r.TotalResults == len(r.Results)}
	if e != nil {
		return result, e
	}
	for _, x := range r.Results {
		addCandidate(&result, model.Candidate{Ref: c.ref(kind, string(x.ID), "tmdb"), Title: first(x.Title, x.Name, x.OriginalTitle, x.OriginalName), OriginalTitle: first(x.OriginalTitle, x.OriginalName), Overview: x.Overview, Year: year(first(x.ReleaseDate, x.FirstAirDate))})
	}
	return result, nil
}
func (c *client) tmdbTitle(ctx context.Context, r model.RemoteRef, p Parts) (model.Title, error) {
	t := model.Title{Artwork: map[string]string{}, CreditsState: "missing", ArtworkState: "missing"}
	path := "/" + tmdbKind(r.Kind) + "/" + r.RemoteID
	if p.Basic {
		var x tmdbRecord
		_, e := c.json(ctx, path+"?language=zh-CN", &x)
		if e != nil {
			return t, e
		}
		if string(x.ID) != r.RemoteID {
			return t, model.ErrMetadata
		}
		t.Title = first(x.Title, x.Name, x.OriginalTitle, x.OriginalName)
		t.OriginalTitle = first(x.OriginalTitle, x.OriginalName)
		t.Overview = x.Overview
		t.Year = year(first(x.ReleaseDate, x.FirstAirDate))
		t.SourceURL = "https://www.themoviedb.org/" + tmdbKind(r.Kind) + "/" + r.RemoteID
		if t.Title == "" || t.Overview == "" {
			var en tmdbRecord
			if _, e = c.json(ctx, path+"?language=en-US", &en); e == nil && string(en.ID) == r.RemoteID {
				t.Title = first(t.Title, en.Title, en.Name, en.OriginalTitle, en.OriginalName)
				t.OriginalTitle = first(t.OriginalTitle, en.OriginalTitle, en.OriginalName)
				t.Overview = first(t.Overview, en.Overview)
			}
		}
		if v := tmdbImage(x.Poster); v != "" {
			t.Artwork["primary"] = v
		}
	}
	if p.Credits {
		var credits struct {
			ID   json.Number `json:"id"`
			Cast []struct {
				ID           json.Number `json:"id"`
				Name         string      `json:"name"`
				OriginalName string      `json:"original_name"`
				Profile      string      `json:"profile_path"`
				Character    string      `json:"character"`
				Order        int         `json:"order"`
				Roles        []struct {
					Character string `json:"character"`
				} `json:"roles"`
			} `json:"cast"`
			Crew []struct {
				ID      json.Number `json:"id"`
				Name    string      `json:"name"`
				Profile string      `json:"profile_path"`
				Job     string      `json:"job"`
				Jobs    []struct {
					Job string `json:"job"`
				} `json:"jobs"`
			} `json:"crew"`
		}
		endpoint := "/credits"
		if r.Kind == "series" {
			endpoint = "/aggregate_credits"
		}
		_, e := c.json(ctx, path+endpoint+"?language=zh-CN", &credits)
		if e == nil && (string(credits.ID) != r.RemoteID || (credits.Cast == nil && credits.Crew == nil)) {
			e = model.ErrProvider
		}
		t.CreditsState = optional(e)
		if e == nil {
			sort.SliceStable(credits.Cast, func(i, j int) bool { return credits.Cast[i].Order < credits.Cast[j].Order })
			for _, x := range credits.Cast {
				chars := []string{}
				if x.Character != "" {
					chars = append(chars, clip(x.Character, 512))
				}
				for _, v := range x.Roles {
					chars = append(chars, clip(v.Character, 512))
				}
				if len(chars) > 50 {
					chars = chars[:50]
				}
				addCredit(&t, model.Credit{Key: string(x.ID), Name: first(x.Name, x.OriginalName), Role: "actor", Photo: tmdbImage(x.Profile), Characters: chars})
			}
			for _, x := range credits.Crew {
				director := x.Job == "Director"
				for _, v := range x.Jobs {
					director = director || v.Job == "Director"
				}
				if director {
					addCredit(&t, model.Credit{Key: string(x.ID), Name: x.Name, Role: "director", Photo: tmdbImage(x.Profile), Characters: []string{}})
				}
			}
		}
	}
	if p.Artwork {
		var images struct {
			ID        json.Number `json:"id"`
			Backdrops []struct {
				Path     string  `json:"file_path"`
				Language string  `json:"iso_639_1"`
				Width    int     `json:"width"`
				Height   int     `json:"height"`
				Score    float64 `json:"vote_average"`
			} `json:"backdrops"`
		}
		_, e := c.json(ctx, path+"/images", &images)
		if e == nil && (string(images.ID) != r.RemoteID || images.Backdrops == nil) {
			e = model.ErrProvider
		}
		t.ArtworkState = optional(e)
		if e == nil {
			for _, kind := range []string{"thumb", "backdrop"} {
				best := -1.0
				for _, x := range images.Backdrops {
					if x.Height == 0 || float64(x.Width)/float64(x.Height) < 1.4 || float64(x.Width)/float64(x.Height) > 2 || tmdbImage(x.Path) == "" {
						continue
					}
					score := x.Score
					if (kind == "thumb" && (x.Language == "zh" || x.Language == "cn")) || (kind == "backdrop" && x.Language == "") {
						score += 100
					}
					if score > best {
						best = score
						t.Artwork[kind] = x.Path
					}
				}
			}
		}
	}
	return t, nil
}
func addCredit(t *model.Title, c model.Credit) {
	if !digits.MatchString(c.Key) {
		c.Key = ""
	}
	if c.Name == "" {
		return
	}
	n := 0
	for _, x := range t.Credits {
		if x.Role == c.Role {
			n++
			if x.Key == c.Key && c.Key != "" {
				return
			}
		}
	}
	if (c.Role == "actor" && n >= 100) || (c.Role == "director" && n >= 50) {
		return
	}
	c.Name = clip(c.Name, 512)
	if c.Characters == nil {
		c.Characters = []string{}
	}
	if len(c.Characters) > 50 {
		c.Characters = c.Characters[:50]
	}
	t.Credits = append(t.Credits, c)
}
