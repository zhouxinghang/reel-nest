package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func loadContract(t *testing.T) *Contract {
	t.Helper()
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func exampleData(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	b, err := os.ReadFile("testdata/examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var examples map[string]json.RawMessage
	if err = json.Unmarshal(b, &examples); err != nil {
		t.Fatal(err)
	}
	return examples
}
func schemaName(ref string) string { return strings.TrimPrefix(ref, "#/components/schemas/") }
func request(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	return r
}
func jsonHeaders() http.Header { return http.Header{"Content-Type": {"application/json"}} }

// Every declared operation must have a real reviewed fixture; missing/new DTOs
// fail until examples are supplied. These are contract fixtures, not proof that
// the legacy application already serves the new interface.
func TestEveryOperationHasValidRequestAndResponseFixture(t *testing.T) {
	c := loadContract(t)
	examples := exampleData(t)
	seen := map[string]bool{}
	operations := 0
	for path, item := range c.doc.Paths.Map() {
		for method, op := range item.Operations() {
			operations++
			t.Run(op.OperationID, func(t *testing.T) {
				if seen[op.OperationID] {
					t.Fatal("duplicate operation ID")
				}
				seen[op.OperationID] = true
				if op.Extensions["x-requirements"] == nil {
					t.Fatal("missing requirement trace")
				}
				u := strings.NewReplacer("{id}", "id-1", "{source}", "tmdb", "{target}", "media", "{number}", "1", "{subtitle}", "sub-1").Replace(path)
				if strings.HasSuffix(path, "/candidates") {
					u += "?q=synthetic&source=tmdb"
				}
				if strings.HasPrefix(path, "/stream/") {
					u += "?token=" + strings.Repeat("a", 64)
				}
				var body string
				if op.RequestBody != nil {
					name := schemaName(op.RequestBody.Value.Content.Get("application/json").Schema.Ref)
					b, ok := examples[name]
					if !ok {
						t.Fatalf("missing request example %s", name)
					}
					body = string(b)
				}
				r := request(method, u, body)
				if err := c.ValidateRequest(context.Background(), r); err != nil {
					t.Fatal(err)
				}
				if body != "" {
					b, _ := io.ReadAll(r.Body)
					if string(b) != body {
						t.Fatal("request body was not restored")
					}
				}
				for status, resp := range op.Responses.Map() {
					if status == "default" {
						continue
					}
					var code int
					fmt.Sscan(status, &code)
					h := jsonHeaders()
					output := ""
					if media := resp.Value.Content.Get("application/json"); media != nil {
						name := schemaName(media.Schema.Ref)
						b, ok := examples[name]
						if !ok {
							t.Fatalf("missing response example %s", name)
						}
						output = string(b)
					} else if method == http.MethodHead {
						h = http.Header{}
					} else {
						for contentType := range resp.Value.Content {
							if contentType == "video/*" {
								contentType = "video/webm"
							}
							h = http.Header{"Content-Type": {contentType}}
							output = "synthetic content"
							break
						}
					}
					if err := c.ValidateResponse(context.Background(), r, code, h, io.NopCloser(strings.NewReader(output))); err != nil {
						t.Fatalf("status %d: %v", code, err)
					}
				}
				if err := c.ValidateResponse(context.Background(), r, 409, jsonHeaders(), io.NopCloser(strings.NewReader(string(examples["Error"])))); err != nil {
					t.Fatal("stable error envelope", err)
				}
			})
		}
	}
	if operations != 39 {
		t.Fatalf("operation inventory changed (%d); review new coverage", operations)
	}
}
func TestRejectsInvalidRequests(t *testing.T) {
	c := loadContract(t)
	cases := []struct{ method, path, body string }{
		{"POST", "/api/login", `{"username":"family","password":"x","extra":true}`},
		{"POST", "/api/login", `{"username":"family","password":"x"} {}`},
		{"POST", "/api/login", `null`},
		{"POST", "/api/login", `{"username":"family"}`},
		{"POST", "/api/login", `{"username":"family","password":"` + strings.Repeat("x", MaxJSONBody) + `"}`},
		{"POST", "/api/libraries", `{"name":"x","path":"/synthetic","kind":"video"}`},
		{"POST", "/api/libraries", `{"name":"x","path":"/synthetic","kind":"movie","sources":["tmdb","tmdb"]}`},
		{"PATCH", "/api/libraries/id-1", `{"revision":"1","name":"x"}`},
		{"PATCH", "/api/libraries/id-1", `{"revision":0,"name":"x"}`},
		{"PUT", "/api/favorites/media/id-1", `{"favorite":null}`},
		{"PUT", "/api/media/id-1/progress", `{"position":1,"duration":0,"completed":false}`},
		{"PUT", "/api/media/id-1/progress", `{"position":NaN,"duration":60,"completed":false}`},
		{"PUT", "/api/metadata/subjects/id-1/selection", `{"subjectRevision":1,"expectedRevision":0,"ref":{"source":"tmdb","catalogId":"tmdb","namespace":"tmdb","kind":"movie","remoteId":42,"providerRevision":1}}`},
		{"POST", "/api/media/id-1/delete", `{"confirmation":"x","confirm":false}`},
		{"GET", "/api/catalog?limit=101", ""}, {"GET", "/api/catalog?limit=0", ""},
		{"GET", "/api/catalog?limit=10&limit=20", ""}, {"GET", "/api/catalog?limit=10;extra=1", ""},
		{"GET", "/api/catalog?tmdbId=2", ""}, {"GET", "/api/catalog?q=" + strings.Repeat("a", 257), ""},
		{"GET", "/api/catalog?cursor=" + strings.Repeat("a", 4097), ""},
		{"GET", "/api/catalog?view=everything", ""}, {"GET", "/api/session", `{}`},
		{"GET", "/stream/id-1?token=bad", ""}, {"GET", "/stream/id-1?token=" + strings.Repeat("a", 64) + "&token=" + strings.Repeat("a", 64), ""},
	}
	for i, tc := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			if err := c.ValidateRequest(context.Background(), request(tc.method, tc.path, tc.body)); err == nil {
				t.Fatalf("accepted %s %s %s", tc.method, tc.path, tc.body)
			}
		})
	}
}
func TestRejectsResponseDriftAndPreservesDeletionPartialSuccess(t *testing.T) {
	c := loadContract(t)
	cases := []struct {
		method, path string
		status       int
		body         string
	}{
		{"GET", "/api/session", 200, `{"username":"family","password":"secret"}`},
		{"GET", "/api/session", 401, `{"error":"old shape"}`},
		{"GET", "/api/session", 401, `{"error":{"code":"UNKNOWN","message":"x","retryable":false,"requestId":"request-1"}}`},
		{"POST", "/api/media/id-1/delete", 200, `{"fileOutcome":"deleted","indexOutcome":"pending","message":"x","retryable":true,"requestId":"request-1"}`},
		{"GET", "/api/catalog", 200, `{"items":[],"page":{"nextCursor":null,"expiresAt":0}}`},
	}
	for _, tc := range cases {
		if err := c.ValidateResponse(context.Background(), request(tc.method, tc.path, ""), tc.status, jsonHeaders(), io.NopCloser(strings.NewReader(tc.body))); err == nil {
			t.Fatal("accepted response drift", tc.body)
		}
	}
	for _, outcome := range []string{"deleted", "not_deleted", "unknown"} {
		body := fmt.Sprintf(`{"fileOutcome":%q,"indexOutcome":"pending","message":"请核查","retryable":false,"requestId":"request-1"}`, outcome)
		if err := c.ValidateResponse(context.Background(), request("POST", "/api/media/id-1/delete", ""), 200, jsonHeaders(), io.NopCloser(strings.NewReader(body))); err != nil {
			t.Fatal(err)
		}
	}
	// A catalog page cannot smuggle whole casts/files or exceed its cardinality.
	examples := exampleData(t)
	var catalog map[string]any
	json.Unmarshal(examples["CatalogPage"], &catalog)
	item := catalog["items"].([]any)[0]
	items := make([]any, 101)
	for i := range items {
		items[i] = item
	}
	catalog["items"] = items
	b, _ := json.Marshal(catalog)
	if err := c.ValidateResponse(context.Background(), request("GET", "/api/catalog", ""), 200, jsonHeaders(), io.NopCloser(strings.NewReader(string(b)))); err == nil {
		t.Fatal("unbounded catalog response accepted")
	}
}
