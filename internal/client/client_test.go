package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(t *testing.T, h http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(Config{Endpoint: srv.URL, Token: "test-token", UserAgent: "terraform-provider-arianet/test", PollInterval: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	c.sleep = func(ctx context.Context, d time.Duration) error { return ctx.Err() }
	return c, srv
}

func TestNewRefusesPlainHTTP(t *testing.T) {
	cases := map[string]bool{
		"http://api.example.com":   false,
		"ftp://api.example.com":    false,
		"https://api.example.com":  true,
		"http://localhost:8080":    true,
		"http://127.0.0.1:9000":    true,
		"http://[::1]:9000":        true,
		"":                         true, // default endpoint
		"https://api.example.com/": true,
	}
	for endpoint, ok := range cases {
		_, err := New(Config{Endpoint: endpoint, Token: "x"})
		if (err == nil) != ok {
			t.Errorf("New(%q) error = %v, want ok=%v", endpoint, err, ok)
		}
	}
	if _, err := New(Config{Endpoint: "https://api.example.com"}); err == nil {
		t.Error("New without a token should fail")
	}
}

func TestHeadersAndPrefix(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cloud/v1/servers/7" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != "terraform-provider-arianet/test" {
			t.Errorf("User-Agent = %q", got)
		}
		_, _ = w.Write([]byte(`{"data":{"id":7,"name":"web","labels":{"env":"prod"}}}`))
	}))
	s, err := c.GetServer(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != 7 || s.Name != "web" || s.Labels["env"] != "prod" {
		t.Fatalf("server = %+v", s)
	}
}

func TestCloudErrorShape(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"Verify your identity to use cloud servers.","code":"identity_needed","reason":"identity_hold"}`))
	}))
	_, err := c.GetServer(context.Background(), 1)
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v", err)
	}
	if apiErr.StatusCode != 403 || apiErr.Code != "identity_needed" || apiErr.Reason != "identity_hold" || !strings.Contains(apiErr.Message, "Verify") {
		t.Fatalf("parsed = %+v", apiErr)
	}
}

func TestGatewayErrorShapeAndNotFound(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"success":false,"error":{"code":"NOT_FOUND","message":"SSH key not found"}}`))
	}))
	_, err := c.GetSSHKey(context.Background(), 3)
	if !IsNotFound(err) {
		t.Fatalf("IsNotFound(%v) = false", err)
	}
	var apiErr *Error
	errors.As(err, &apiErr)
	if apiErr.Code != "NOT_FOUND" || apiErr.Message != "SSH key not found" {
		t.Fatalf("parsed = %+v", apiErr)
	}
}

func TestNonJSONError(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`<html>oops</html>`))
	}))
	_, err := c.GetServer(context.Background(), 1)
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 500 || apiErr.Message == "" {
		t.Fatalf("err = %#v", err)
	}
}

func TestRetries429WithRetryAfter(t *testing.T) {
	var calls int32
	var waited []time.Duration
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"slow down","code":"rate_limit_exceeded"}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"id":5},"action_id":9}`))
	}))
	c.sleep = func(_ context.Context, d time.Duration) error { waited = append(waited, d); return nil }
	_, actionID, err := c.CreateVolume(context.Background(), CreateVolumeRequest{Name: "v", Datacenter: "x", SizeGB: 10})
	if err != nil {
		t.Fatal(err)
	}
	if actionID != 9 || calls != 3 {
		t.Fatalf("actionID=%d calls=%d", actionID, calls)
	}
	if len(waited) != 2 || waited[0] != 7*time.Second {
		t.Fatalf("waited = %v", waited)
	}
}

func TestRetryIsBounded(t *testing.T) {
	var calls int32
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Retry-After", "100000")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	var waited []time.Duration
	c.sleep = func(_ context.Context, d time.Duration) error { waited = append(waited, d); return nil }
	_, err := c.GetServer(context.Background(), 1)
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 429 {
		t.Fatalf("err = %v", err)
	}
	if int(calls) != defaultMaxRetries+1 {
		t.Fatalf("calls = %d", calls)
	}
	for _, d := range waited {
		if d > maxRetryAfter {
			t.Fatalf("waited %v, more than the cap", d)
		}
	}
}

func TestRetries503OnReadsOnly(t *testing.T) {
	var calls int32
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if r.Method == http.MethodGet && n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"down","code":"unavailable"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"id":1}}`))
	}))
	c.sleep = func(context.Context, time.Duration) error { return nil }
	if _, err := c.GetServer(context.Background(), 1); err != nil {
		t.Fatalf("GET should have been retried: %v", err)
	}
	atomic.StoreInt32(&calls, 0)
	if _, err := c.DeleteVolume(context.Background(), 1); err == nil {
		t.Fatal("DELETE must not be retried on 503")
	}
	if calls != 1 {
		t.Fatalf("DELETE calls = %d", calls)
	}
}

func TestPagination(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("per_page") != "100" {
			t.Errorf("per_page = %q", q.Get("per_page"))
		}
		if q.Get("label_selector") != "env=prod" {
			t.Errorf("label_selector = %q", q.Get("label_selector"))
		}
		page, _ := strconv.Atoi(q.Get("page"))
		var next any
		if page < 3 {
			next = page + 1
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"id": page*10 + 1}, {"id": page*10 + 2}},
			"meta": map[string]any{"pagination": map[string]any{"page": page, "per_page": 100, "next_page": next, "last_page": 3, "total_entries": 6}},
		})
	}))
	servers, err := c.ListServers(context.Background(), "env=prod")
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 6 || servers[5].ID != 32 {
		t.Fatalf("servers = %+v", servers)
	}
}

func TestOmitsEmptyQuery(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("label_selector") || r.URL.Query().Has("type") {
			t.Errorf("empty filters sent: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"data":[],"meta":{"pagination":{"page":1,"next_page":null}}}`))
	}))
	if _, err := c.ListImages(context.Background(), "", "", ""); err != nil {
		t.Fatal(err)
	}
}

func TestWaitActionSuccess(t *testing.T) {
	var polls int32
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cloud/v1/actions/42" {
			t.Errorf("path = %s", r.URL.Path)
		}
		status := "running"
		if atomic.AddInt32(&polls, 1) >= 3 {
			status = "success"
		}
		_, _ = w.Write([]byte(`{"data":{"id":42,"command":"create","status":"` + status + `","progress":50}}`))
	}))
	c.sleep = func(context.Context, time.Duration) error { return nil }
	if err := c.WaitAction(context.Background(), 42); err != nil {
		t.Fatal(err)
	}
	if polls != 3 {
		t.Fatalf("polls = %d", polls)
	}
}

func TestWaitActionError(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"id":5,"command":"create","status":"error","resource":{"type":"server","id":3},"error":{"code":"no_capacity","message":"The datacenter is full."}}}`))
	}))
	err := c.WaitAction(context.Background(), 5)
	var af *ActionFailedError
	if !errors.As(err, &af) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "no_capacity") || !strings.Contains(err.Error(), "The datacenter is full.") {
		t.Fatalf("message = %s", err)
	}
}

func TestWaitActionTimeout(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"id":5,"command":"create","status":"running"}}`))
	}))
	c.sleep = sleepCtx
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := c.WaitAction(ctx, 5)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
}

func TestWaitActionZeroIsNoop(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request expected")
	}))
	if err := c.WaitAction(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
}

func TestRefusesCrossHostRedirect(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("token leaked to another host")
		}
		_, _ = w.Write([]byte(`{"data":{"id":1}}`))
	}))
	defer other.Close()
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, strings.Replace(other.URL, "127.0.0.1", "localhost", 1)+r.URL.Path, http.StatusFound)
	}))
	if _, err := c.GetServer(context.Background(), 1); err == nil {
		t.Fatal("cross-host redirect was followed")
	}
}

func TestSSHKeyEnvelopeAndPaging(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/ssh-keys") {
			t.Errorf("path = %s", r.URL.Path)
		}
		switch r.Method {
		case http.MethodGet:
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success":    true,
				"data":       []map[string]any{{"id": page, "name": "k" + strconv.Itoa(page), "key": "ssh-ed25519 AAAA"}},
				"pagination": map[string]any{"current_page": page, "per_page": 100, "total": 2, "last_page": 2},
			})
		case http.MethodPost:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["name"] != "laptop" || body["public_key"] == "" {
				t.Errorf("body = %v", body)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":11,"name":"laptop","key":"ssh-ed25519 AAAA","fingerprint":"SHA256:x","is_default":false}}`))
		}
	}))
	keys, err := c.ListSSHKeys(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || keys[1].NameOrEmpty() != "k2" {
		t.Fatalf("keys = %+v", keys)
	}
	k, err := c.CreateSSHKey(context.Background(), CreateSSHKeyRequest{Name: "laptop", PublicKey: "ssh-ed25519 AAAA"})
	if err != nil {
		t.Fatal(err)
	}
	if k.ID != 11 || k.Fingerprint != "SHA256:x" {
		t.Fatalf("key = %+v", k)
	}
}

func TestDeleteServerBodyOnlyWhenAsked(t *testing.T) {
	var bodies []string
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 256)
		n, _ := r.Body.Read(buf)
		bodies = append(bodies, string(buf[:n]))
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"action_id":3}`))
	}))
	if _, err := c.DeleteServer(context.Background(), 1, DeleteServerOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.DeleteServer(context.Background(), 1, DeleteServerOptions{KeepBackups: true}); err != nil {
		t.Fatal(err)
	}
	if bodies[0] != "" || !strings.Contains(bodies[1], `"keep_backups":true`) {
		t.Fatalf("bodies = %q", bodies)
	}
}

func TestRetryAfterParsing(t *testing.T) {
	if d := retryAfter("3"); d != 3*time.Second {
		t.Errorf("3 -> %v", d)
	}
	if d := retryAfter("9999"); d != maxRetryAfter {
		t.Errorf("cap -> %v", d)
	}
	if d := retryAfter(time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)); d != 0 {
		t.Errorf("past date -> %v", d)
	}
	if d := retryAfter("garbage"); d != 0 {
		t.Errorf("garbage -> %v", d)
	}
}
