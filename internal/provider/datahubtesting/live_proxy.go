// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// A lost update needs a write to land between the provider's read of an
// aspect and its write of it. Provoking that with timing is the #97 lesson
// (a race the in-process mock could never lose, and CI lost once), so the
// live race tests do not race: they put this proxy between the provider and
// the real DataHub, and it runs the competing write at the only moment that
// matters, after the server has served the provider's read-for-write and
// before the provider sees it.
type InterceptingProxy struct {
	*httptest.Server
	proxy *httputil.ReverseProxy

	mu sync.Mutex
	// onFirstGet maps urn + "|" + aspect to the function to run once, after
	// the next GET of that aspect with systemMetadata.
	onFirstGet map[string]func()
	// strip removes every aspect's "headers" object from POST bodies, which
	// is the control arm: the provider's write without its precondition.
	strip bool
	posts []ProxyPost
}

// ProxyPost is one aspect of one OpenAPI v3 POST as it left the proxy.
type ProxyPost struct {
	URN           string
	Aspect        string
	HeaderPresent bool
	Status        int
}

// NewInterceptingProxy starts a proxy in front of upstream, a DataHub GMS
// base URL. The caller's Authorization header is forwarded unchanged.
func NewInterceptingProxy(t *testing.T, upstream string) *InterceptingProxy {
	t.Helper()
	target, err := url.Parse(upstream)
	if err != nil {
		t.Fatalf("parsing upstream %q: %v", upstream, err)
	}
	p := &InterceptingProxy{onFirstGet: map[string]func(){}}
	p.proxy = &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.Out.Host = target.Host
			// SetURL drops the incoming Authorization header only through
			// SetXForwarded; keep the client's headers as they are.
		},
	}
	p.Server = httptest.NewServer(p)
	t.Cleanup(p.Close)
	return p
}

// OnFirstGet runs fn once, synchronously, after the upstream has answered
// the next GET of aspect on urn that asks for systemMetadata (the provider's
// read-for-write) and before that answer reaches the provider.
func (p *InterceptingProxy) OnFirstGet(urn, aspect string, fn func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onFirstGet[urn+"|"+aspect] = fn
}

// StripVersionHeaders makes the proxy drop every per-aspect "headers" object
// from POST bodies from now on: the provider's writes reach DataHub without
// their If-Version-Match, which is how the control arm shows the hazard the
// precondition exists for.
func (p *InterceptingProxy) StripVersionHeaders() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.strip = true
}

// Posts returns every aspect of every OpenAPI v3 POST the proxy forwarded.
func (p *InterceptingProxy) Posts() []ProxyPost {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]ProxyPost(nil), p.posts...)
}

func (p *InterceptingProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/openapi/v3/entity/") {
		p.proxy.ServeHTTP(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		rec := httptest.NewRecorder()
		p.proxy.ServeHTTP(rec, r)
		if r.URL.Query().Get("systemMetadata") == "true" {
			for _, aspect := range strings.Split(r.URL.Query().Get("aspects"), ",") {
				p.fire(r.URL.Path, aspect)
			}
		}
		copyRecorded(w, rec, rec.Body.Bytes())
	case http.MethodPost:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		p.mu.Lock()
		strip := p.strip
		p.mu.Unlock()
		posted := parsePosted(body)
		if strip {
			body = stripAspectHeaders(body)
			for i := range posted {
				posted[i].headerPresent = false
			}
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		rec := httptest.NewRecorder()
		p.proxy.ServeHTTP(rec, r)
		p.mu.Lock()
		for _, a := range posted {
			p.posts = append(p.posts, ProxyPost{URN: a.urn, Aspect: a.aspect, HeaderPresent: a.headerPresent, Status: rec.Code})
		}
		p.mu.Unlock()
		copyRecorded(w, rec, rec.Body.Bytes())
	default:
		p.proxy.ServeHTTP(w, r)
	}
}

// fire runs and disarms the hook for the urn in path and aspect, if any.
func (p *InterceptingProxy) fire(path, aspect string) {
	p.mu.Lock()
	var fn func()
	var key string
	for k, f := range p.onFirstGet {
		urn := strings.SplitN(k, "|", 2)[0]
		if strings.HasSuffix(k, "|"+aspect) && strings.Contains(path, url.PathEscape(urn)) || strings.HasSuffix(k, "|"+aspect) && strings.Contains(path, urn) {
			fn, key = f, k
			break
		}
	}
	if fn != nil {
		delete(p.onFirstGet, key)
	}
	p.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func stripAspectHeaders(body []byte) []byte {
	var entities []map[string]json.RawMessage
	if err := json.Unmarshal(body, &entities); err != nil {
		return body
	}
	for _, entity := range entities {
		for aspect, raw := range entity {
			if aspect == "urn" {
				continue
			}
			var a map[string]json.RawMessage
			if err := json.Unmarshal(raw, &a); err != nil {
				continue
			}
			delete(a, "headers")
			out, err := json.Marshal(a)
			if err != nil {
				continue
			}
			entity[aspect] = out
		}
	}
	out, err := json.Marshal(entities)
	if err != nil {
		return body
	}
	return out
}
