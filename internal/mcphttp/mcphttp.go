// Package mcphttp serves an MCP handler over MCP's Streamable HTTP
// transport, statelessly (docs/architecture.md, §13): each POST carries
// one JSON-RPC message and, for a request, gets its response as JSON.
// The server issues no session ids and opens no event streams: mcp-docs'
// tools need no state between calls and send nothing unasked, so any
// number of instances can serve one endpoint and a restart loses
// nothing.
//
// What the transport checks, as the specification asks: the Origin of
// browser requests (against DNS rebinding), the protocol version a
// client names, the content type, and bounds on body size, concurrent
// requests and time.
package mcphttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/sdrahn/mcp-docs/internal/mcpserver"
)

// Options configure the transport.
type Options struct {
	// Path is the MCP endpoint (default "/mcp").
	Path string
	// AllowedOrigins are the browser origins ("https://app.example") a
	// request may come from. A request without an Origin header (any
	// client that is not a browser) is always allowed; one with another
	// Origin is refused.
	AllowedOrigins []string
	// MaxBody bounds a request's body (default 1 MiB).
	MaxBody int64
	// MaxConcurrent bounds the requests handled at once (default 32);
	// more wait for a slot as long as their client does.
	MaxConcurrent int
	// Timeout bounds the handling of one request (default 60 s).
	Timeout time.Duration
}

func (o *Options) defaults() {
	if o.Path == "" {
		o.Path = "/mcp"
	}
	if o.MaxBody <= 0 {
		o.MaxBody = 1 << 20
	}
	if o.MaxConcurrent <= 0 {
		o.MaxConcurrent = 32
	}
	if o.Timeout <= 0 {
		o.Timeout = 60 * time.Second
	}
}

// HealthPath answers 200 while the server runs, for container health
// checks and load balancers.
const HealthPath = "/healthz"

type message struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      json.RawMessage  `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  any              `json:"result,omitempty"`
	Error   *mcpserver.Error `json:"error,omitempty"`
}

// Handler returns the HTTP handler of the endpoint and the health path.
func Handler(h mcpserver.Handler, o Options) http.Handler {
	o.defaults()
	t := &transport{h: h, o: o, slots: make(chan struct{}, o.MaxConcurrent)}
	mux := http.NewServeMux()
	mux.HandleFunc(o.Path, t.serve)
	mux.HandleFunc(HealthPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "ok\n")
	})
	return mux
}

type transport struct {
	h     mcpserver.Handler
	o     Options
	slots chan struct{}
}

func (t *transport) serve(w http.ResponseWriter, r *http.Request) {
	if origin := r.Header.Get("Origin"); origin != "" && !slices.Contains(t.o.AllowedOrigins, origin) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodPost:
	case http.MethodGet, http.MethodDelete:
		// No server-initiated stream to open, no session to end.
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "this server is stateless: POST JSON-RPC messages", http.StatusMethodNotAllowed)
		return
	default:
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(strings.ToLower(strings.TrimSpace(ct)), "application/json") {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	// A client names the protocol version it negotiated on every request
	// after initialize; none means 2025-03-26, which is spoken.
	if v := r.Header.Get("MCP-Protocol-Version"); v != "" && !slices.Contains(mcpserver.ProtocolVersions, v) {
		http.Error(w, "unsupported MCP-Protocol-Version "+v, http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, t.o.MaxBody))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "reading the request: "+err.Error(), http.StatusBadRequest)
		return
	}
	body = bytes.TrimSpace(body)
	if len(body) > 0 && body[0] == '[' {
		// Batches were removed from MCP in 2025-06-18.
		t.reply(w, http.StatusBadRequest, message{ID: json.RawMessage("null"),
			Error: &mcpserver.Error{Code: mcpserver.CodeInvalidRequest, Message: "batches are not supported: send one message per request"}})
		return
	}
	var m message
	if err := json.Unmarshal(body, &m); err != nil {
		t.reply(w, http.StatusBadRequest, message{ID: json.RawMessage("null"),
			Error: &mcpserver.Error{Code: mcpserver.CodeParse, Message: "parse error"}})
		return
	}
	switch {
	case m.Method == "" && len(m.ID) > 0, m.Method != "" && len(m.ID) == 0:
		// A response, or a notification (initialized, cancelled: a request
		// of this transport ends with its HTTP request): nothing to answer.
		w.WriteHeader(http.StatusAccepted)
		return
	case m.Method == "":
		t.reply(w, http.StatusBadRequest, message{ID: json.RawMessage("null"),
			Error: &mcpserver.Error{Code: mcpserver.CodeInvalidRequest, Message: "invalid request"}})
		return
	}

	// The request ends when its client goes away, or at the timeout.
	ctx, cancel := context.WithTimeout(r.Context(), t.o.Timeout)
	defer cancel()
	select {
	case t.slots <- struct{}{}:
		defer func() { <-t.slots }()
	case <-ctx.Done():
		http.Error(w, "busy", http.StatusServiceUnavailable)
		return
	}
	res, err := t.h(ctx, m.Method, m.Params)
	if ctx.Err() != nil && r.Context().Err() != nil {
		return // the client is gone: nobody to answer
	}
	resp := message{ID: m.ID}
	var rerr *mcpserver.Error
	switch {
	case errors.As(err, &rerr):
		resp.Error = rerr
	case errors.Is(err, context.DeadlineExceeded):
		resp.Error = &mcpserver.Error{Code: mcpserver.CodeInternal, Message: "timed out"}
	case err != nil:
		resp.Error = &mcpserver.Error{Code: mcpserver.CodeInternal, Message: err.Error()}
	default:
		resp.Result = res
	}
	t.reply(w, http.StatusOK, resp)
}

func (t *transport) reply(w http.ResponseWriter, status int, m message) {
	m.JSONRPC = "2.0"
	b, err := json.Marshal(m)
	if err != nil {
		http.Error(w, "encoding the response: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(b, '\n'))
}
