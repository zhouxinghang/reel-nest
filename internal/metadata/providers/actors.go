package providers

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"reelnest/internal/model"
)

type metaActor struct {
	ID           string   `json:"id"`
	Provider     string   `json:"provider"`
	Name         string   `json:"name"`
	Aliases      []string `json:"aliases"`
	Summary      string   `json:"summary"`
	Homepage     string   `json:"homepage"`
	Birthday     string   `json:"birthday"`
	DebutDate    string   `json:"debut_date"`
	Nationality  string   `json:"nationality"`
	Height       int      `json:"height"`
	BloodType    string   `json:"blood_type"`
	Hobby        string   `json:"hobby"`
	Skill        string   `json:"skill"`
	Measurements string   `json:"measurements"`
	CupSize      string   `json:"cup_size"`
	Images       []string `json:"images"`
}

func ValidActorRef(r model.ActorRef) bool {
	return r.CatalogID != "" && r.ProviderRevision > 0 && namespace.MatchString(r.Namespace) &&
		strings.TrimSpace(r.RemoteID) != "" && len(r.RemoteID) <= 1024 && utf8.ValidString(r.RemoteID) &&
		!strings.ContainsFunc(r.RemoteID, unicode.IsControl) && r.RemoteID != "." && r.RemoteID != ".."
}

func (c *client) validActor(r model.ActorRef) bool {
	return c.config.Source() == "metatube" && c.config.Configured() && ValidActorRef(r) &&
		r.CatalogID == c.config.CatalogID() && r.ProviderRevision == c.config.Revision()
}

func actorStrings(values []string, limit int) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		value = clip(value, 256)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
			if len(result) == limit {
				break
			}
		}
	}
	return result
}

func metaError(e json.RawMessage) bool {
	return len(e) > 0 && string(e) != "null" && string(e) != `""`
}

func (c *client) actorCandidate(a metaActor) model.ActorCandidate {
	return model.ActorCandidate{
		Ref:  model.ActorRef{CatalogID: c.config.CatalogID(), Namespace: strings.ToLower(a.Provider), RemoteID: a.ID, ProviderRevision: c.config.Revision()},
		Name: clip(a.Name, 256), Aliases: actorStrings(a.Aliases, 30),
	}
}

func (c *client) SearchActors(ctx context.Context, q string) (model.ActorSearch, error) {
	result := model.ActorSearch{Items: []model.ActorCandidate{}}
	if c.config.Source() != "metatube" || !c.config.Configured() {
		return result, model.ErrProvider
	}
	var reply struct {
		Data  []metaActor     `json:"data"`
		Error json.RawMessage `json:"error"`
	}
	status, err := c.json(ctx, "/actors/search?"+url.Values{"q": {q}, "fallback": {"true"}}.Encode(), &reply)
	if status == 404 {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if reply.Data == nil || metaError(reply.Error) {
		return result, model.ErrProvider
	}
	seen := map[model.ActorRef]bool{}
	for _, a := range reply.Data {
		item := c.actorCandidate(a)
		if !c.validActor(item.Ref) || item.Name == "" {
			result.Truncated = true
			continue
		}
		if seen[item.Ref] {
			continue
		}
		seen[item.Ref] = true
		if len(result.Items) == 50 {
			result.Truncated = true
			break
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func actorDate(s string) string {
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if d, err := time.Parse(layout, s); err == nil && d.Year() > 1 {
			return d.Format("2006-01-02")
		}
	}
	return ""
}

func (c *client) FetchActor(ctx context.Context, ref model.ActorRef) (model.ActorProfile, error) {
	var result model.ActorProfile
	if !c.validActor(ref) {
		return result, model.ErrCandidate
	}
	var reply struct {
		Data  metaActor       `json:"data"`
		Error json.RawMessage `json:"error"`
	}
	status, err := c.json(ctx, "/actors/"+url.PathEscape(ref.Namespace)+"/"+url.PathEscape(ref.RemoteID)+"?lazy=true", &reply)
	if status == 404 {
		return result, model.ErrNotFound
	}
	if err != nil {
		return result, err
	}
	a := reply.Data
	if metaError(reply.Error) {
		return result, model.ErrProvider
	}
	result.ActorCandidate = c.actorCandidate(a)
	if result.Ref != ref || result.Name == "" {
		return result, model.ErrMetadata
	}
	result.Summary, result.Homepage = clip(a.Summary, 32768), safePage(a.Homepage)
	result.Birthday, result.DebutDate = actorDate(a.Birthday), actorDate(a.DebutDate)
	result.Nationality, result.BloodType = clip(a.Nationality, 256), clip(a.BloodType, 256)
	result.Hobby, result.Skill = clip(a.Hobby, 1024), clip(a.Skill, 1024)
	result.Measurements, result.CupSize = clip(a.Measurements, 256), clip(a.CupSize, 256)
	if a.Height > 0 && a.Height < 300 {
		result.Height = strconv.Itoa(a.Height)
	}
	result.Images = []string{}
	seen := map[string]bool{}
	for _, image := range a.Images {
		// These references are sent only to the configured MetaTube image API;
		// ReelNest never requests these origins or follows their redirects.
		if image = safePage(image); image != "" && !seen[image] {
			seen[image] = true
			result.Images = append(result.Images, image)
			if len(result.Images) == 20 {
				break
			}
		}
	}
	return result, nil
}

func (c *client) FetchActorImage(ctx context.Context, ref model.ActorRef, image string, original bool) ([]byte, string, error) {
	if !c.validActor(ref) || (image != "" && safePage(image) == "") {
		return nil, "", model.ErrAsset
	}
	query := url.Values{}
	if original {
		query.Set("ratio", "0") // Actor providers do not support backdrop/thumb.
	}
	if image != "" {
		query.Set("url", image)
	}
	endpoint := c.config.BaseURL() + "/images/primary/" + url.PathEscape(ref.Namespace) + "/" + url.PathEscape(ref.RemoteID)
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	return c.fetchImage(ctx, endpoint)
}
