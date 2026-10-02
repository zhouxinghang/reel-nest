// Package api provides the versioned OpenAPI document and reusable HTTP contract
// checks. It does not authenticate requests and is not yet mounted by the old app.
package api

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"
)

//go:embed openapi.yaml
var document []byte

const MaxJSONBody = 16 << 10

type Contract struct {
	doc    *openapi3.T
	router routers.Router
}

func Load() (*Contract, error) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(document)
	if err != nil {
		return nil, err
	}
	if err = doc.Validate(context.Background()); err != nil {
		return nil, err
	}
	router, err := legacy.NewRouter(doc)
	if err != nil {
		return nil, err
	}
	return &Contract{doc: doc, router: router}, nil
}

func (c *Contract) requestInput(r *http.Request) (*openapi3filter.RequestValidationInput, error) {
	route, params, err := c.router.FindRoute(r)
	if err != nil {
		return nil, err
	}
	return &openapi3filter.RequestValidationInput{Request: r, PathParams: params, Route: route, Options: &openapi3filter.Options{
		// Deliberately not an authorization implementation. Real middleware must
		// check sessions, same origin and stream tokens before invoking handlers.
		AuthenticationFunc:    openapi3filter.NoopAuthenticationFunc,
		IncludeResponseStatus: true,
	}}, nil
}

// ValidateRequest checks shape, limits, unknown/duplicate queries and trailing
// JSON. It restores the body for explicit handler DTO decoding. Business rules
// (filesystem containment, version CAS, token validity) remain in their modules.
func (c *Contract) ValidateRequest(ctx context.Context, r *http.Request) error {
	input, err := c.requestInput(r)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, list := range []openapi3.Parameters{input.Route.PathItem.Parameters, input.Route.Operation.Parameters} {
		for _, p := range list {
			if p.Value.In == "query" {
				known[p.Value.Name] = true
			}
		}
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return err
	}
	for key, values := range query {
		if !known[key] || len(values) != 1 {
			return fmt.Errorf("unknown or repeated query parameter %q", key)
		}
	}
	if r.Body != nil {
		body, err := io.ReadAll(io.LimitReader(r.Body, MaxJSONBody+1))
		_ = r.Body.Close()
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		if err != nil {
			return err
		}
		if len(body) > MaxJSONBody {
			return errors.New("JSON body exceeds 16 KiB")
		}
		if len(body) > 0 {
			if input.Route.Operation.RequestBody == nil {
				return errors.New("unexpected request body")
			}
			if !json.Valid(body) {
				return errors.New("invalid or trailing JSON")
			}
		}
	}
	return openapi3filter.ValidateRequest(ctx, input)
}

// ValidateResponse validates explicit DTO output (including binary/HEAD/Range
// representations). A request need not pass auth here: error responses are tested
// through the same seam. Callers must not mutate request concurrently.
func (c *Contract) ValidateResponse(ctx context.Context, r *http.Request, status int, header http.Header, body io.ReadCloser) error {
	input, err := c.requestInput(r)
	if err != nil {
		return err
	}
	return openapi3filter.ValidateResponse(ctx, &openapi3filter.ResponseValidationInput{RequestValidationInput: input, Status: status, Header: header, Body: body, Options: input.Options})
}
