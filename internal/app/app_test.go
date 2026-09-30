package app_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"

	"reelnest/internal/app"
)

const password = "test-household-password"

type runningApp struct {
	application *app.App
	server      *httptest.Server
	client      *http.Client
}

func start(t *testing.T, config app.Config) *runningApp {
	t.Helper()
	application, err := app.New(config)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler())
	jar, _ := cookiejar.New(nil)
	running := &runningApp{application, server, &http.Client{Jar: jar}}
	t.Cleanup(running.close)
	return running
}

func (a *runningApp) close() {
	if a.server != nil {
		a.server.Close()
		a.server = nil
		_ = a.application.Close()
	}
}

func request(t *testing.T, client *http.Client, method, url string, body any, status int) ([]byte, *http.Response) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(method, url, bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != status {
		t.Fatalf("%s %s: got %d, want %d: %s", method, url, response.StatusCode, status, data)
	}
	return data, response
}

func login(t *testing.T, a *runningApp) *http.Cookie {
	t.Helper()
	_, response := request(t, a.client, "POST", a.server.URL+"/api/login", map[string]string{"username": "family", "password": password}, 200)
	if len(response.Cookies()) != 1 {
		t.Fatal("expected a session cookie")
	}
	return response.Cookies()[0]
}

func TestHouseholdLoginAndLogout(t *testing.T) {
	a := start(t, app.Config{DataDir: t.TempDir(), Username: "family", Password: password})
	request(t, a.client, "GET", a.server.URL+"/api/library", nil, 401)
	request(t, a.client, "GET", a.server.URL+"/api/session", nil, 401)
	request(t, a.client, "POST", a.server.URL+"/api/login", map[string]string{"username": "family", "password": "wrong"}, 401)
	cookie := login(t, a)
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" {
		t.Fatalf("unsafe cookie: %+v", cookie)
	}
	data, _ := request(t, a.client, "GET", a.server.URL+"/api/library", nil, 200)
	var library struct {
		Items []any `json:"items"`
	}
	if err := json.Unmarshal(data, &library); err != nil || library.Items == nil || len(library.Items) != 0 {
		t.Fatalf("expected empty library: %s", data)
	}
	request(t, a.client, "POST", a.server.URL+"/api/logout", map[string]string{}, 200)
	request(t, a.client, "GET", a.server.URL+"/api/library", nil, 401)
	req, _ := http.NewRequest("GET", a.server.URL+"/api/library", nil)
	req.AddCookie(cookie)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("logged-out session can be replayed")
	}
}

func TestPersistedHouseholdSurvivesRestartAndSharesState(t *testing.T) {
	dir := t.TempDir()
	a := start(t, app.Config{DataDir: dir, Username: "family", Password: password})
	login(t, a)
	first, _ := request(t, a.client, "GET", a.server.URL+"/api/session", nil, 200)
	a.close()
	// No bootstrap credentials are supplied on restart.
	b := start(t, app.Config{DataDir: dir})
	login(t, b)
	second, _ := request(t, b.client, "GET", b.server.URL+"/api/session", nil, 200)
	if !bytes.Equal(first, second) {
		t.Fatalf("household changed after restart: %s != %s", first, second)
	}
	jar, _ := cookiejar.New(nil)
	other := &runningApp{client: &http.Client{Jar: jar}, server: b.server}
	login(t, other)
	one, _ := request(t, b.client, "GET", b.server.URL+"/api/library", nil, 200)
	two, _ := request(t, other.client, "GET", b.server.URL+"/api/library", nil, 200)
	if !bytes.Equal(one, two) {
		t.Fatal("browsers do not share the library")
	}
	request(t, other.client, "POST", b.server.URL+"/api/logout", map[string]string{}, 200)
	request(t, b.client, "GET", b.server.URL+"/api/library", nil, 200)
}

func TestLoginRejectsCrossSiteAndMalformedRequests(t *testing.T) {
	a := start(t, app.Config{DataDir: t.TempDir(), Username: "family", Password: password})
	for _, tc := range []struct {
		name, body, contentType, origin string
		want                            int
	}{
		{"cross-site", `{"username":"family","password":"` + password + `"}`, "application/json", "https://untrusted.example", 403},
		{"form", "username=family&password=" + password, "application/x-www-form-urlencoded", "", 415},
		{"trailing-json", `{"username":"family","password":"` + password + `"} {}`, "application/json", "", 400},
		{"unknown-field", `{"username":"family","password":"` + password + `","admin":true}`, "application/json", "", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest("POST", a.server.URL+"/api/login", bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", tc.contentType)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			response, err := a.client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != tc.want {
				t.Fatalf("got %d, want %d", response.StatusCode, tc.want)
			}
		})
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	_, response := request(t, client, "GET", a.server.URL+"/library", nil, 303)
	if response.Header.Get("Location") != "/login" {
		t.Fatal("protected page did not redirect to login")
	}
	login(t, a)
	data, _ := request(t, a.client, "GET", a.server.URL+"/api/session", nil, 200)
	if bytes.Contains(data, []byte(password)) || bytes.Contains(data, []byte("hash")) {
		t.Fatal("credentials exposed")
	}
}
