package app

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// Person IDs are scoped to the containing title's source, not globally unique.
type CreditPerson struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Key       string `json:"key,omitempty"`
	PhotoURL  string `json:"photoUrl,omitempty"`
	PhotoPath string `json:"photoPath,omitempty"` // Validated provider artwork reference; never supplied by a request.
}

type CastMember struct {
	CreditPerson
	Roles []string `json:"roles"`
}

type TitleCredits struct {
	Version   int            `json:"version,omitempty"`
	Status    string         `json:"status"` // available (including empty) or failed
	Cast      []CastMember   `json:"cast"`
	Directors []CreditPerson `json:"directors"`
	FetchedAt int64          `json:"fetchedAt"`
	RetryAt   int64          `json:"retryAt,omitempty"`
	Error     string         `json:"error,omitempty"`
	Truncated bool           `json:"truncated,omitempty"`
}

func creditsDue(credits *TitleCredits) bool {
	return credits == nil || ((credits.Version < 1 || credits.Status == "failed") && credits.RetryAt <= time.Now().Unix())
}

func fetchTitleCredits(ctx context.Context, client metadataProvider, kind string, id int64, previous *TitleCredits) *TitleCredits {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	credits, err := client.credits(ctx, kind, id)
	credits.FetchedAt = time.Now().Unix()
	if err != nil {
		// Supplementary data must not invalidate a title, trigger cross-source
		// fallback, or retry on every scan/restart. The next eligible job retries.
		credits = TitleCredits{Status: "failed", FetchedAt: credits.FetchedAt, RetryAt: time.Now().Add(24 * time.Hour).Unix(), Error: err.Error()}
		if previous != nil && previous.Status == "available" {
			// Upgrading an old credits payload must not erase names/roles if only
			// the supplemental photo lookup fails. Retain its version for retry.
			retained := *previous
			retained.RetryAt, retained.Error = credits.RetryAt, credits.Error
			return &retained
		}
	} else {
		credits.Status = "available"
		credits.Version = 1
	}
	if credits.Cast == nil {
		credits.Cast = []CastMember{}
	}
	if credits.Directors == nil {
		credits.Directors = []CreditPerson{}
	}
	return &credits
}

func fetchMetadataDetails(ctx context.Context, client metadataProvider, kind string, id int64) (TitleMetadata, []byte, string, string, error) {
	m, poster, typ, warning, err := client.details(ctx, kind, id)
	if err == nil {
		// Some providers return complete artwork/credits in their detail record.
		if artworkDue(m.Artwork) {
			m.Artwork = fetchTitleArtwork(ctx, client, kind, id, m.Artwork)
		}
		decorateArtwork(&m)
		// Supplemental artwork and credits must leave basic metadata usable,
		// even if an extended record is oversized or the provider times out.
		if creditsDue(m.Credits) {
			m.Credits = fetchTitleCredits(ctx, client, kind, id, m.Credits)
		}
		decoratePeople(&m)
	}
	return m, poster, typ, warning, err
}

type creditEntry struct {
	CreditPerson
	Roles    []string
	Director bool
}

// Keep provider billing order; merge repeated roles by source-local person ID.
// Names only identify entries when the provider omitted an ID. Bound the saved
// lists so a large series does not inflate every library/episode response.
func normalizeCredits(entries []creditEntry) TitleCredits {
	result := TitleCredits{Cast: []CastMember{}, Directors: []CreditPerson{}}
	cast, directors := map[string]int{}, map[string]bool{}
	rolesSeen := map[string]map[string]bool{}
	photos := map[string]string{}
	for _, entry := range entries {
		if entry.PhotoPath != "" {
			photos[creditIdentity(entry.CreditPerson)] = entry.PhotoPath
		}
	}
	for _, entry := range entries {
		entry.Name = strings.TrimSpace(entry.Name)
		if entry.Name == "" {
			continue
		}
		key := creditIdentity(entry.CreditPerson)
		entry.PhotoPath = photos[key]
		if entry.ID <= 0 {
			entry.ID = 0
		}
		if entry.Director {
			if !directors[key] {
				if len(result.Directors) == 50 {
					result.Truncated = true
					continue
				}
				directors[key] = true
				result.Directors = append(result.Directors, entry.CreditPerson)
			}
			continue
		}
		index, exists := cast[key]
		if !exists {
			if len(result.Cast) == 100 {
				result.Truncated = true
				continue
			}
			index = len(result.Cast)
			cast[key] = index
			rolesSeen[key] = map[string]bool{}
			result.Cast = append(result.Cast, CastMember{CreditPerson: entry.CreditPerson, Roles: []string{}})
		}
		for _, role := range entry.Roles {
			role = strings.TrimSpace(role)
			if role == "" {
				continue
			}
			if !rolesSeen[key][role] {
				rolesSeen[key][role] = true
				result.Cast[index].Roles = append(result.Cast[index].Roles, role)
			}
		}
	}
	return result
}

func creditIdentity(person CreditPerson) string {
	if person.ID > 0 {
		return "id:" + strconv.FormatInt(person.ID, 10)
	}
	return "name:" + strings.ToLower(strings.TrimSpace(person.Name))
}
