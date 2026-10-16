package mcphttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sdrahn/mcp-docs/internal/mcpserver"
)

// echo answers ping with {}, echo with its params, fail with a
// JSON-RPC error, boom with another error, and wait until cancelled.
func echo(started chan<- struct{}) mcpserver.Handler {
	return func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		switch method {
		case "ping":
			return map[string]any{}, nil
		case "echo":
			return params, nil
		case "fail":
			return nil, &mcpserver.Error{Code: mcpserver.CodeMethodNotFound, Message: "method not found"}
		case "boom":
			return nil, errors.New("boom")
		case "wait":
			if started != nil {
				started <- struct{}{}
			}
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return nil, &mcpserver.Error{Code: mcpserver.CodeMethodNotFound, Message: "method not found"}
	}
}

type reply struct {
	status int
	header http.Header
	body   string
}

func post(t *testing.T, srv *httptest.Server, body string, header map[string]string) reply {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range header {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return reply{resp.StatusCode, resp.Header, string(b)}
}

func TestRequests(t *testing.T) {
	srv := httptest.NewServer(Handler(echo(nil), Options{}))
	defer srv.Close()

	r := post(t, srv, `{"jsonrpc":"2.0","id":7,"method":"echo","params":{"a":1}}`, nil)
	if r.status != 200 || r.header.Get("Content-Type") != "application/json" ||
		r.body != `{"jsonrpc":"2.0","id":7,"result":{"a":1}}`+"\n" {
		t.Errorf("request: %d %q %v", r.status, r.body, r.header)
	}
	if r.header.Get("Mcp-Session-Id") != "" {
		t.Error("a stateless server sent a session id")
	}
	if r := post(t, srv, `{"jsonrpc":"2.0","id":"x","method":"ping"}`, nil); r.body != `{"jsonrpc":"2.0","id":"x","result":{}}`+"\n" {
		t.Errorf("ping: %q", r.body)
	}
	if r := post(t, srv, `{"jsonrpc":"2.0","id":1,"method":"fail"}`, nil); r.status != 200 ||
		r.body != `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"method not found"}}`+"\n" {
		t.Errorf("JSON-RPC error: %d %q", r.status, r.body)
	}
	if r := post(t, srv, `{"jsonrpc":"2.0","id":1,"method":"boom"}`, nil); !strings.Contains(r.body, `"code":-32603,"message":"boom"`) {
		t.Errorf("internal error: %q", r.body)
	}
	// Notifications and responses: accepted, no body.
	for _, b := range []string{`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":3}}`,
		`{"jsonrpc":"2.0","id":5,"result":{}}`} {
		if r := post(t, srv, b, nil); r.status != 202 || r.body != "" {
			t.Errorf("%s: %d %q", b, r.status, r.body)
		}
	}
	// The protocol versions spoken are accepted, others not.
	if r := post(t, srv, `{"jsonrpc":"2.0","id":1,"method":"ping"}`, map[string]string{"MCP-Protocol-Version": "2025-06-18"}); r.status != 200 {
		t.Errorf("known version: %d", r.status)
	}
	if r := post(t, srv, `{"jsonrpc":"2.0","id":1,"method":"ping"}`, map[string]string{"MCP-Protocol-Version": "1999-01-01"}); r.status != 400 {
		t.Errorf("unknown version: %d", r.status)
	}
}

func TestRefusals(t *testing.T) {
	srv := httptest.NewServer(Handler(echo(nil), Options{MaxBody: 64, AllowedOrigins: []string{"https://ok.example"}}))
	defer srv.Close()
	ping := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	for _, tc := range []struct {
		name   string
		body   string
		header map[string]string
		status int
		want   string
	}{
		{"foreign origin", ping, map[string]string{"Origin": "https://evil.example"}, 403, "origin not allowed"},
		{"content type", ping, map[string]string{"Content-Type": "text/plain"}, 415, "application/json"},
		{"too large", `{"jsonrpc":"2.0","id":1,"method":"echo","params":"` + strings.Repeat("x", 100) + `"}`, nil, 413, "too large"},
		{"batch", `[` + ping + `]`, nil, 400, "batches are not supported"},
		{"not JSON", `{nope`, nil, 400, `"code":-32700`},
		{"no method", `{"jsonrpc":"2.0"}`, nil, 400, `"code":-32600`},
	} {
		if r := post(t, srv, tc.body, tc.header); r.status != tc.status || !strings.Contains(r.body, tc.want) {
			t.Errorf("%s: %d %q", tc.name, r.status, r.body)
		}
	}
	if r := post(t, srv, ping, map[string]string{"Origin": "https://ok.example"}); r.status != 200 {
		t.Errorf("allowed origin: %d %q", r.status, r.body)
	}
	for _, m := range []string{http.MethodGet, http.MethodDelete, http.MethodPut} {
		req, _ := http.NewRequest(m, srv.URL+"/mcp", nil)
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != 405 || resp.Header.Get("Allow") != "POST" {
			t.Errorf("%s: %d %v", m, resp.StatusCode, resp.Header)
		}
	}
	for path, want := range map[string]int{"/healthz": 200, "/other": 404} {
		resp, err := srv.Client().Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("%s: %d", path, resp.StatusCode)
		}
	}
}

// A request ends when its client goes away, or at the timeout.
func TestCancellation(t *testing.T) {
	started := make(chan struct{}, 1)
	var done atomic.Int32
	h := echo(started)
	wrapped := func(ctx context.Context, m string, p json.RawMessage) (any, error) {
		res, err := h(ctx, m, p)
		done.Add(1)
		return res, err
	}
	srv := httptest.NewServer(Handler(wrapped, Options{Timeout: 200 * time.Millisecond}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"wait"}`))
	req.Header.Set("Content-Type", "application/json")
	errc := make(chan error, 1)
	go func() {
		resp, err := srv.Client().Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
		errc <- err
	}()
	<-started
	cancel()
	if err := <-errc; err == nil {
		t.Error("the cancelled request got an answer")
	}
	deadline := time.Now().Add(2 * time.Second)
	for done.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if done.Load() == 0 {
		t.Error("the handler did not end when its client went away")
	}

	// The timeout answers with an error.
	go func() { <-started }()
	if r := post(t, srv, `{"jsonrpc":"2.0","id":2,"method":"wait"}`, nil); !strings.Contains(r.body, "timed out") {
		t.Errorf("timeout: %d %q", r.status, r.body)
	}
}

// At most MaxConcurrent requests run at once; others wait.
func TestConcurrency(t *testing.T) {
	var running, peak atomic.Int32
	h := func(ctx context.Context, m string, p json.RawMessage) (any, error) {
		n := running.Add(1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		running.Add(-1)
		return map[string]any{}, nil
	}
	srv := httptest.NewServer(Handler(h, Options{MaxConcurrent: 2}))
	defer srv.Close()
	errc := make(chan error, 6)
	for i := 0; i < 6; i++ {
		go func() {
			r := post(t, srv, `{"jsonrpc":"2.0","id":1,"method":"ping"}`, nil)
			if r.status != 200 {
				errc <- errors.New(r.body)
				return
			}
			errc <- nil
		}()
	}
	for i := 0; i < 6; i++ {
		if err := <-errc; err != nil {
			t.Error(err)
		}
	}
	if peak.Load() > 2 {
		t.Errorf("%d requests ran at once", peak.Load())
	}
}
