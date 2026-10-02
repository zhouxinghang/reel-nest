package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

var errMetadataUnauthorized = errors.New("unauthorized")
var errMetadataNotFound = errors.New("metadata not found")

// Credentials and response bodies never appear in errors. Each upstream has an
// independent rate gate and cooldown, so one failing source cannot block another.
type metadataHTTP struct {
	name, apiURL, imageURL string
	client                 *http.Client
	gate                   chan struct{}
	mu                     sync.Mutex
	next, cooldown         time.Time
}

func (c *metadataHTTP) cooldownUntil() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return max(0, c.cooldown.Unix())
}

func newMetadataHTTP(name, apiURL, imageURL string, timeout time.Duration) (*metadataHTTP, error) {
	return newMetadataHTTPWithPolicy(name, apiURL, imageURL, timeout, false)
}

// Only explicitly configured self-hosted services may use non-loopback HTTP.
// Keep the stricter HTTPS policy for the built-in public providers.
func newMetadataHTTPWithPolicy(name, apiURL, imageURL string, timeout time.Duration, selfHosted bool) (*metadataHTTP, error) {
	for _, endpoint := range []string{apiURL, imageURL} {
		u, err := url.Parse(endpoint)
		if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (selfHosted || u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"))) {
			if selfHosted {
				return nil, errors.New(name + " 服务地址必须是 HTTP/HTTPS，且不含用户名密码、查询参数或片段")
			}
			return nil, errors.New(name + " 服务地址必须是 HTTPS，或仅用于隔离测试的本机 HTTP 地址")
		}
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &metadataHTTP{name: name, apiURL: strings.TrimRight(apiURL, "/"), imageURL: strings.TrimRight(imageURL, "/"), gate: make(chan struct{}, 1), client: &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *metadataHTTP) request(ctx context.Context, method, endpoint, token string, body io.Reader, limit int64) ([]byte, string, error) {
	select {
	case c.gate <- struct{}{}:
		defer func() { <-c.gate }()
	case <-ctx.Done():
		return nil, "", errors.New(c.name + " 请求已取消或超时")
	}
	c.mu.Lock()
	cooldown, next := c.cooldown, c.next
	c.mu.Unlock()
	if time.Now().Before(cooldown) {
		return nil, "", errors.New(c.name + " 请求处于冷却期，请稍后重试；本地播放不受影响")
	}
	if delay := time.Until(next); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return nil, "", errors.New(c.name + " 请求已取消或超时")
		}
	}
	c.mu.Lock()
	c.next = time.Now().Add(250 * time.Millisecond)
	c.mu.Unlock()
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, "", errors.New(c.name + " 请求地址无效")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json, image/jpeg, image/png, image/webp")
	response, err := c.client.Do(req)
	if err != nil {
		return nil, "", errors.New(c.name + " 网络不可用或请求超时；本地播放不受影响")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		if response.StatusCode == 429 || response.StatusCode >= 500 || response.StatusCode == 401 || response.StatusCode == 403 {
			delay := time.Minute
			if response.StatusCode == 429 {
				if seconds, err := strconv.ParseInt(response.Header.Get("Retry-After"), 10, 64); err == nil && seconds >= 0 {
					// Avoid integer overflow; never send another request sooner than advised.
					if seconds > int64((time.Duration(1<<63-1))/time.Second) {
						seconds = int64((time.Duration(1<<63 - 1)) / time.Second)
					}
					if time.Duration(seconds)*time.Second > delay {
						delay = time.Duration(seconds) * time.Second
					}
				} else if date, err := http.ParseTime(response.Header.Get("Retry-After")); err == nil && time.Until(date) > delay {
					delay = time.Until(date)
				}
			}
			c.mu.Lock()
			c.cooldown = time.Now().Add(delay)
			c.mu.Unlock()
		}
		switch response.StatusCode {
		case 401, 403:
			return nil, "", fmt.Errorf("%s 凭据无效或无权限，请部署者检查设置: %w", c.name, errMetadataUnauthorized)
		case 429:
			return nil, "", errors.New(c.name + " 请求受限，已进入冷却期，请稍后重试")
		case 404:
			return nil, "", fmt.Errorf("%s 未找到该作品: %w", c.name, errMetadataNotFound)
		default:
			return nil, "", errors.New(c.name + " 服务错误；本地浏览与播放不受影响")
		}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, "", errors.New(c.name + " 响应不完整或过大")
	}
	return data, response.Header.Get("Content-Type"), nil
}
