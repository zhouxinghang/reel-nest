package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Resolve TVDB artwork type IDs from their catalog rather than confusing
// banners, clearlogos, posters or thumbnails with landscape backgrounds.
func (c *tvdbClient) backgroundTypes(ctx context.Context) (map[int64]string, error) {
	c.artworkMu.Lock()
	defer c.artworkMu.Unlock()
	if c.artworkTypes != nil {
		return c.artworkTypes, nil
	}
	var result struct {
		Data []struct {
			ID         int64  `json:"id"`
			Name       string `json:"name"`
			Slug       string `json:"slug"`
			RecordType string `json:"recordType"`
		} `json:"data"`
	}
	if err := c.json(ctx, "/artwork/types", url.Values{}, &result); err != nil {
		return nil, err
	}
	types := map[int64]string{}
	for _, typ := range result.Data {
		name := strings.ToLower(typ.Name + " " + typ.Slug)
		if strings.Contains(name, "background") || strings.Contains(name, "fanart") || strings.Contains(name, "backdrop") {
			if typ.RecordType == "movie" || typ.RecordType == "series" {
				types[typ.ID] = typ.RecordType
			}
		}
	}
	if len(types) == 0 {
		return nil, errors.New("TVDB 缺少有效的背景图片类型")
	}
	c.artworkTypes = types
	return types, nil
}

func (c *tvdbClient) artwork(ctx context.Context, kind string, id int64) (TitleArtwork, error) {
	types, err := c.backgroundTypes(ctx)
	if err != nil {
		return TitleArtwork{}, err
	}
	var result struct {
		Data struct {
			ID       int64 `json:"id"`
			Artworks []struct {
				Type         int64   `json:"type"`
				Image        string  `json:"image"`
				Language     string  `json:"language"`
				IncludesText bool    `json:"includesText"`
				Width        int     `json:"width"`
				Height       int     `json:"height"`
				Score        float64 `json:"score"`
			} `json:"artworks"`
		} `json:"data"`
	}
	err = c.json(ctx, fmt.Sprintf("/%s/%d/extended", tvdbKind(kind), id), url.Values{"short": {"false"}}, &result)
	if err != nil {
		return TitleArtwork{}, err
	}
	if result.Data.ID != id {
		return TitleArtwork{}, errors.New("TVDB 返回的图片作品标识不符")
	}
	candidates := []artworkCandidate{}
	for _, img := range result.Data.Artworks {
		if types[img.Type] != kind || img.Height <= 0 || float64(img.Width)/float64(img.Height) < 1.4 || float64(img.Width)/float64(img.Height) > 2.0 || strings.TrimSpace(img.Image) == "" || len(img.Image) > 2048 {
			continue
		}
		if ref := c.artworkURL(img.Image); ref != "" {
			candidates = append(candidates, artworkCandidate{ref: ref, language: img.Language, text: img.IncludesText, score: img.Score, width: img.Width})
		}
	}
	return selectArtwork(candidates), nil
}

func (c *tvdbClient) image(ctx context.Context, ref, imageType string) ([]byte, string, error) {
	endpoint := c.artworkURL(ref)
	if endpoint == "" || ref == "" {
		return nil, "", errors.New("图片地址无效")
	}
	return c.imageHTTP.request(ctx, "GET", endpoint, "", nil, 5<<20)
}
