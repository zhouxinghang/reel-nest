package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
)

func (c *tmdbClient) artwork(ctx context.Context, kind string, id int64) (TitleArtwork, error) {
	var result struct {
		ID        int64 `json:"id"`
		Backdrops []struct {
			Path     string  `json:"file_path"`
			Language string  `json:"iso_639_1"`
			Width    int     `json:"width"`
			Height   int     `json:"height"`
			Score    float64 `json:"vote_average"`
		} `json:"backdrops"`
	}
	// TMDB has no separate Thumb category: choose a localized backdrop for the
	// cover and a language-neutral backdrop for the immersive background.
	err := c.json(ctx, fmt.Sprintf("/%s/%d/images", tmdbKind(kind), id), url.Values{}, &result)
	if err != nil {
		return TitleArtwork{}, err
	}
	if result.ID != id || result.Backdrops == nil {
		return TitleArtwork{}, errors.New("TMDB 返回无效的图片资料")
	}
	candidates := []artworkCandidate{}
	for _, img := range result.Backdrops {
		if !posterPath.MatchString(img.Path) || img.Height <= 0 || float64(img.Width)/float64(img.Height) < 1.4 || float64(img.Width)/float64(img.Height) > 2.0 {
			continue
		}
		candidates = append(candidates, artworkCandidate{ref: img.Path, language: img.Language, text: img.Language != "", score: img.Score, width: img.Width})
	}
	return selectArtwork(candidates), nil
}

func (c *tmdbClient) image(ctx context.Context, ref, imageType string) ([]byte, string, error) {
	if !posterPath.MatchString(ref) {
		return nil, "", errors.New("图片地址无效")
	}
	client := c.backdropHTTP
	if imageType == "thumb" {
		client = c.thumbHTTP
	}
	return client.request(ctx, "GET", client.imageURL+ref, "", nil, 5<<20)
}
