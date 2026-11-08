// Package client is a small HTTP client for the arianet Cloud API
// (cloud-service behind the gateway at <endpoint>/cloud/v1) and for the
// account-wide SSH keys served by arianet-api at <endpoint>/api/v1.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultEndpoint is the public API gateway.
const DefaultEndpoint = "https://api.ariaservice.net"

const (
	cloudPrefix = "/cloud/v1"
	corePrefix  = "/api/v1"

	defaultMaxRetries   = 5
	defaultPollInterval = 2 * time.Second
	maxPollInterval     = 10 * time.Second
	maxRetryAfter       = 60 * time.Second
	maxPerPage          = 100
)

// Config configures a Client.
type Config struct {
	// Endpoint is the gateway root, e.g. https://api.ariaservice.net.
	Endpoint string
	// Token is a bearer API token.
	Token string
	// UserAgent is sent on every request.
	UserAgent string
	// HTTPClient is optional; its CheckRedirect is replaced.
	HTTPClient *http.Client
	// MaxRetries bounds retries of 429 (any method) and 502/503/504 (reads).
	MaxRetries int
	// PollInterval is the first wait between action polls.
	PollInterval time.Duration
}

// Client talks to the arianet API.
type Client struct {
	base         *url.URL
	token        string
	userAgent    string
	http         *http.Client
	maxRetries   int
	pollInterval time.Duration
	// sleep is swapped in tests.
	sleep func(ctx context.Context, d time.Duration) error
}

// New validates the configuration and returns a Client. Plain http is
// refused unless the host is a loopback address or localhost.
func New(cfg Config) (*Client, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/")
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("invalid endpoint %q", endpoint)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !isLocalHost(u.Hostname()) {
			return nil, fmt.Errorf("endpoint %q uses plain http; only https is allowed except for localhost", endpoint)
		}
	default:
		return nil, fmt.Errorf("endpoint %q must use https", endpoint)
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, errors.New("an API token is required")
	}
	hc := &http.Client{Timeout: 60 * time.Second}
	if cfg.HTTPClient != nil {
		copied := *cfg.HTTPClient
		hc = &copied
	}
	hc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("stopped after 5 redirects")
		}
		first := via[0].URL
		if req.URL.Host != first.Host || req.URL.Scheme != first.Scheme {
			return fmt.Errorf("refusing redirect from %s to another host %s", first.Host, req.URL.Host)
		}
		return nil
	}
	c := &Client{
		base:         u,
		token:        cfg.Token,
		userAgent:    cfg.UserAgent,
		http:         hc,
		maxRetries:   cfg.MaxRetries,
		pollInterval: cfg.PollInterval,
		sleep:        sleepCtx,
	}
	if c.userAgent == "" {
		c.userAgent = "terraform-provider-arianet/dev"
	}
	if c.maxRetries <= 0 {
		c.maxRetries = defaultMaxRetries
	}
	if c.pollInterval <= 0 {
		c.pollInterval = defaultPollInterval
	}
	return c, nil
}

func isLocalHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Error is a non-2xx answer of the API.
type Error struct {
	StatusCode int
	Code       string
	Message    string
	Reason     string
}

func (e *Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "arianet API: HTTP %d", e.StatusCode)
	if e.Code != "" {
		fmt.Fprintf(&b, " (%s", e.Code)
		if e.Reason != "" {
			fmt.Fprintf(&b, ", %s", e.Reason)
		}
		b.WriteString(")")
	}
	if e.Message != "" {
		b.WriteString(": ")
		b.WriteString(e.Message)
	}
	return b.String()
}

// IsNotFound reports whether err is a 404 of the API.
func IsNotFound(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.StatusCode == http.StatusNotFound
}

// parseError understands cloud-service's {"error":"msg","code","reason"} and
// the gateway's {"success":false,"error":{"code","message"}}.
func parseError(status int, body []byte) *Error {
	e := &Error{StatusCode: status}
	var raw map[string]json.RawMessage
	if json.Unmarshal(body, &raw) != nil {
		e.Message = strings.TrimSpace(truncate(string(body), 300))
		if e.Message == "" {
			e.Message = http.StatusText(status)
		}
		return e
	}
	if v, ok := raw["error"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			e.Message = s
		} else {
			var obj struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			if json.Unmarshal(v, &obj) == nil {
				e.Code, e.Message = obj.Code, obj.Message
			}
		}
	}
	if v, ok := raw["code"]; ok && e.Code == "" {
		_ = json.Unmarshal(v, &e.Code)
	}
	if v, ok := raw["reason"]; ok {
		_ = json.Unmarshal(v, &e.Reason)
	}
	if v, ok := raw["message"]; ok && e.Message == "" {
		_ = json.Unmarshal(v, &e.Message)
	}
	if e.Message == "" {
		e.Message = http.StatusText(status)
	}
	return e
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// request is one API call.
type request struct {
	method string
	path   string // full path including prefix
	query  url.Values
	body   any
}

// do sends req and decodes a 2xx JSON answer into out (when non-nil).
func (c *Client) do(ctx context.Context, req request, out any) error {
	var payload []byte
	if req.body != nil {
		var err error
		if payload, err = json.Marshal(req.body); err != nil {
			return fmt.Errorf("encoding request: %w", err)
		}
	}
	u := *c.base
	u.Path = strings.TrimRight(c.base.Path, "/") + req.path
	u.RawQuery = ""
	if len(req.query) > 0 {
		u.RawQuery = req.query.Encode()
	}
	read := req.method == http.MethodGet || req.method == http.MethodHead

	for attempt := 0; ; attempt++ {
		var bodyReader io.Reader
		if payload != nil {
			bodyReader = bytes.NewReader(payload)
		}
		hr, err := http.NewRequestWithContext(ctx, req.method, u.String(), bodyReader)
		if err != nil {
			return err
		}
		hr.Header.Set("Authorization", "Bearer "+c.token)
		hr.Header.Set("Accept", "application/json")
		hr.Header.Set("User-Agent", c.userAgent)
		if payload != nil {
			hr.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.http.Do(hr)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if read && attempt < c.maxRetries {
				if serr := c.sleep(ctx, backoff(attempt)); serr != nil {
					return serr
				}
				continue
			}
			return fmt.Errorf("%s %s: %w", req.method, req.path, err)
		}
		data, rerr := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		if rerr != nil {
			return fmt.Errorf("%s %s: reading response: %w", req.method, req.path, rerr)
		}

		retryable := resp.StatusCode == http.StatusTooManyRequests ||
			(read && (resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusGatewayTimeout))
		if retryable && attempt < c.maxRetries {
			wait := retryAfter(resp.Header.Get("Retry-After"))
			if wait <= 0 {
				wait = backoff(attempt)
			}
			if serr := c.sleep(ctx, wait); serr != nil {
				return serr
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return parseError(resp.StatusCode, data)
		}
		if out == nil || len(bytes.TrimSpace(data)) == 0 {
			return nil
		}
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("%s %s: decoding response: %w", req.method, req.path, err)
		}
		return nil
	}
}

// backoff is 1s, 2s, 4s ... capped at 30s.
func backoff(attempt int) time.Duration {
	d := time.Duration(math.Pow(2, float64(attempt))) * time.Second
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	return d
}

// retryAfter reads Retry-After as seconds or an HTTP date, capped.
func retryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	var d time.Duration
	if n, err := strconv.Atoi(v); err == nil {
		d = time.Duration(n) * time.Second
	} else if t, err := http.ParseTime(v); err == nil {
		d = time.Until(t)
	}
	if d < 0 {
		d = 0
	}
	if d > maxRetryAfter {
		d = maxRetryAfter
	}
	return d
}

// Cloud calls the Cloud API: path is relative to <endpoint>/cloud/v1.
func (c *Client) Cloud(ctx context.Context, method, path string, query url.Values, body, out any) error {
	return c.do(ctx, request{method: method, path: cloudPrefix + path, query: query, body: body}, out)
}

// coreEnvelope is arianet-api's {"success","data","error"} envelope.
type coreEnvelope struct {
	Success    bool            `json:"success"`
	Data       json.RawMessage `json:"data"`
	Pagination *struct {
		CurrentPage int   `json:"current_page"`
		PerPage     int   `json:"per_page"`
		Total       int64 `json:"total"`
		LastPage    int   `json:"last_page"`
	} `json:"pagination"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// core calls arianet-api (path relative to <endpoint>/api/v1) and unwraps
// its envelope into out.
func (c *Client) core(ctx context.Context, method, path string, query url.Values, body, out any) (*coreEnvelope, error) {
	var env coreEnvelope
	if err := c.do(ctx, request{method: method, path: corePrefix + path, query: query, body: body}, &env); err != nil {
		return nil, err
	}
	if !env.Success && env.Error != nil {
		return nil, &Error{StatusCode: http.StatusOK, Code: env.Error.Code, Message: env.Error.Message}
	}
	if out != nil && len(env.Data) > 0 && string(env.Data) != "null" {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return nil, fmt.Errorf("%s %s: decoding data: %w", method, path, err)
		}
	}
	return &env, nil
}

// Pagination is meta.pagination of a cloud list.
type Pagination struct {
	Page         int  `json:"page"`
	PerPage      int  `json:"per_page"`
	PreviousPage *int `json:"previous_page"`
	NextPage     *int `json:"next_page"`
	LastPage     int  `json:"last_page"`
	TotalEntries int  `json:"total_entries"`
}

type listPage[T any] struct {
	Data []T `json:"data"`
	Meta struct {
		Pagination *Pagination `json:"pagination"`
	} `json:"meta"`
}

// listAll pages through a cloud list (?page=&per_page=100) and returns every item.
func listAll[T any](ctx context.Context, c *Client, path string, query url.Values) ([]T, error) {
	q := url.Values{}
	for k, v := range query {
		if len(v) > 0 && v[0] != "" {
			q[k] = v
		}
	}
	q.Set("per_page", strconv.Itoa(maxPerPage))
	var out []T
	page := 1
	for i := 0; i < 10000; i++ {
		q.Set("page", strconv.Itoa(page))
		var p listPage[T]
		if err := c.Cloud(ctx, http.MethodGet, path, q, nil, &p); err != nil {
			return nil, err
		}
		out = append(out, p.Data...)
		pg := p.Meta.Pagination
		if pg == nil || pg.NextPage == nil || *pg.NextPage <= page {
			break
		}
		page = *pg.NextPage
	}
	if out == nil {
		out = []T{}
	}
	return out, nil
}

// one is {"data": T}.
type one[T any] struct {
	Data T `json:"data"`
}

// actionRef is {"action_id": n}, sometimes beside "data".
type actionRef struct {
	ActionID int64 `json:"action_id"`
}

func idPath(prefix string, id int64) string {
	return prefix + "/" + strconv.FormatInt(id, 10)
}
