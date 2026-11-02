// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// An OpenAPI v3 aspect write replaces the whole stored aspect, so a write that
// sends only the fields the provider models erases every other field. That
// shipped in four resources at once (data product assets, glossary term
// sources, structured property definitions, corp user profiles) because
// nothing forced anyone to decide, per aspect, whether the provider owns every
// field or must read the aspect and carry the rest over. The tests in this file
// make that decision mandatory.
//
// TestEveryOpenAPIAspectWriteIsClassified finds every function in this package
// that POSTs to the OpenAPI v3 entity endpoint and fails on any missing from
// aspectWriters. TestOpenAPIAspectWritesKeepUnmanagedFields then runs each
// classified writer against a server whose stored aspects carry a field no
// provider version models, and checks the POST names exactly the classified
// aspects and writes that field back for every aspect classified as
// preserved. TestOpenAPIAspectWritesStopOnFailedRead checks that a failed read
// of a preserved aspect stops the write instead of sending only the managed
// fields.

type aspectOwnership int

const (
	// preserveUnmanaged: the writer reads the aspect with
	// unmanagedAspectFields and replaces only the fields it manages.
	preserveUnmanaged aspectOwnership = iota
	// ownWholeAspect: the writer deliberately replaces the whole aspect. The
	// reason must say why nothing outside the provider's fields can be lost.
	ownWholeAspect
)

type aspectRule struct {
	ownership aspectOwnership
	reason    string
}

type aspectWriter struct {
	// aspects is every aspect the writer can send, by aspect name.
	aspects map[string]aspectRule
	// write invokes the writer so that it sends every aspect in aspects.
	write func(ctx context.Context, c *Client) error
}

func preserved() aspectRule { return aspectRule{ownership: preserveUnmanaged} }

func ownedWhole(reason string) aspectRule {
	return aspectRule{ownership: ownWholeAspect, reason: reason}
}

const keyAspectReason = "key aspect: holds only the identity fields the URN is built from"

// aspectWriters classifies every OpenAPI v3 aspect write in this package, keyed
// by the name of the function that issues it.
var aspectWriters = map[string]aspectWriter{
	"UpsertCorpUser": {
		aspects: map[string]aspectRule{
			"corpUserKey":  ownedWhole(keyAspectReason),
			"corpUserInfo": preserved(),
			"subTypes": ownedWhole("typeNames is the aspect's only field, and it is written only " +
				"for service accounts, whose subtype the resource owns"),
		},
		write: func(ctx context.Context, c *Client) error {
			_, err := c.UpsertCorpUser(ctx, UpsertCorpUserInput{
				Username: "guard", FullName: "Guard", SubTypes: []string{"SERVICE_ACCOUNT"},
			})
			return err
		},
	},
	"WriteDataProductProperties": {
		aspects: map[string]aspectRule{
			"dataProductProperties": preserved(),
			"domains": ownedWhole("the resource owns the data product's domain; the server " +
				"derives domainAssociations from the previous value when only domains is sent"),
		},
		write: func(ctx context.Context, c *Client) error {
			return c.WriteDataProductProperties(ctx, "urn:li:dataProduct:guard", "Guard", "", "", nil, "urn:li:domain:guard")
		},
	},
	"SetDomainProperties": {
		aspects: map[string]aspectRule{"domainProperties": preserved()},
		write: func(ctx context.Context, c *Client) error {
			return c.SetDomainProperties(ctx, "urn:li:domain:guard", "Guard", "", "", nil)
		},
	},
	"SetGlobalTags": {
		aspects: map[string]aspectRule{
			"globalTags": ownedWhole("tags is the aspect's only field, and the resources own the " +
				"complete tag list by design"),
		},
		write: func(ctx context.Context, c *Client) error {
			return c.SetGlobalTags(ctx, "tag", "urn:li:tag:guard", []string{"urn:li:tag:other"})
		},
	},
	"setGlossaryProperties": {
		aspects: map[string]aspectRule{
			"glossaryTermInfo": preserved(),
			"glossaryNodeInfo": preserved(),
		},
		write: func(ctx context.Context, c *Client) error {
			if err := c.setGlossaryProperties(ctx, "glossaryterm", "glossaryTermInfo",
				"urn:li:glossaryTerm:guard", "Guard", "def", "", nil, nil); err != nil {
				return err
			}
			return c.setGlossaryProperties(ctx, "glossarynode", "glossaryNodeInfo",
				"urn:li:glossaryNode:guard", "Guard", "def", "", nil, nil)
		},
	},
	"NewDatasourceIngestion": {
		aspects: map[string]aspectRule{
			"dataHubIngestionSourceKey":  ownedWhole(keyAspectReason),
			"dataHubIngestionSourceInfo": preserved(),
		},
		write: func(ctx context.Context, c *Client) error {
			recipe := `{"source":{"type":"mysql"}}`
			_, err := c.NewDatasourceIngestion(ctx, DatasourceIngestionInput{
				SourceID: "guard", SourceName: "Guard", SourceType: "mysql", RecipeJSON: &recipe,
			})
			return err
		},
	},
	"WriteOwnershipTypeInfo": {
		aspects: map[string]aspectRule{"ownershipTypeInfo": preserved()},
		write: func(ctx context.Context, c *Client) error {
			return c.WriteOwnershipTypeInfo(ctx, "urn:li:ownershipType:guard", "Guard", "")
		},
	},
	"writeStructuredProperty": {
		aspects: map[string]aspectRule{
			"propertyDefinition":         preserved(),
			"structuredPropertySettings": preserved(),
		},
		write: func(ctx context.Context, c *Client) error {
			return c.writeStructuredProperty(ctx, "urn:li:structuredProperty:io.guard", CreateStructuredPropertyInput{
				ID: "io.guard", ValueType: "string", EntityTypes: []string{"dataset"},
				Settings: &StructuredPropertySettings{},
			}, false)
		},
	},
	"WriteTagProperties": {
		aspects: map[string]aspectRule{
			"tagProperties": ownedWhole("name, description and colorHex are every field the aspect " +
				"has, and the resource manages all three"),
		},
		write: func(ctx context.Context, c *Client) error {
			return c.WriteTagProperties(ctx, "urn:li:tag:guard", "Guard", "", "")
		},
	},
}

// openAPIAspectWriteFuncs returns the name of every function in this package's
// non-test sources that calls NewRequest with http.MethodPost and a path under
// /openapi/v3/entity/, given either as a literal or through a local variable
// assigned from a literal or fmt.Sprintf.
func openAPIAspectWriteFuncs(t *testing.T) map[string]string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("listing package sources: %v", err)
	}
	fset := token.NewFileSet()
	found := map[string]string{}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			paths := openAPIPathVars(fn.Body)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) < 3 {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "NewRequest" || !isMethodPost(call.Args[1]) {
					return true
				}
				if isOpenAPIEntityPath(call.Args[2]) || isPathVar(call.Args[2], paths) {
					found[fn.Name.Name] = fset.Position(call.Pos()).String()
				}
				return true
			})
		}
	}
	return found
}

func isMethodPost(e ast.Expr) bool {
	if sel, ok := e.(*ast.SelectorExpr); ok {
		return sel.Sel.Name == "MethodPost"
	}
	if lit, ok := e.(*ast.BasicLit); ok {
		return lit.Value == `"POST"`
	}
	return false
}

// isOpenAPIEntityPath reports whether e is a string literal, or a fmt.Sprintf
// call whose format is one, naming a path under /openapi/v3/entity/.
func isOpenAPIEntityPath(e ast.Expr) bool {
	if call, ok := e.(*ast.CallExpr); ok && len(call.Args) > 0 {
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Sprintf" {
			e = call.Args[0]
		}
	}
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	s, err := strconv.Unquote(lit.Value)
	return err == nil && strings.HasPrefix(s, "/openapi/v3/entity/")
}

// openAPIPathVars returns the local variables in body assigned an OpenAPI v3
// entity path.
func openAPIPathVars(body *ast.BlockStmt) map[string]bool {
	vars := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != len(as.Rhs) {
			return true
		}
		for i, lhs := range as.Lhs {
			if id, ok := lhs.(*ast.Ident); ok && isOpenAPIEntityPath(as.Rhs[i]) {
				vars[id.Name] = true
			}
		}
		return true
	})
	return vars
}

func isPathVar(e ast.Expr, vars map[string]bool) bool {
	id, ok := e.(*ast.Ident)
	return ok && vars[id.Name]
}

// aspectWriteCallers returns the name of every function in this package's
// non-test sources that calls writeAspects, which is where every aspect
// write must go through.
func aspectWriteCallers(t *testing.T) map[string]string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("listing package sources: %v", err)
	}
	fset := token.NewFileSet()
	found := map[string]string{}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "writeAspects" {
					found[fn.Name.Name] = fset.Position(call.Pos()).String()
				}
				return true
			})
		}
	}
	return found
}

// TestOnlyTheSharedBuilderPostsAspects pins the one function that may POST to
// the OpenAPI v3 entity endpoint. Any other is a write that bypasses the
// version precondition and the retry.
func TestOnlyTheSharedBuilderPostsAspects(t *testing.T) {
	found := openAPIAspectWriteFuncs(t)
	delete(found, "postAspects")
	for name, pos := range found {
		t.Errorf("%s (%s) POSTs to the OpenAPI v3 entity endpoint directly; route it through "+
			"writeAspects, which carries If-Version-Match for every aspect it read and retries on a conflict", name, pos)
	}
}

func TestEveryOpenAPIAspectWriteIsClassified(t *testing.T) {
	found := aspectWriteCallers(t)
	if len(found) == 0 {
		t.Fatal("found no writeAspects callers at all; the source scan is broken, not the package clean")
	}
	for name, pos := range found {
		if _, ok := aspectWriters[name]; !ok {
			t.Errorf("%s (%s) writes aspects but is not in aspectWriters. "+
				"An aspect write replaces the whole stored aspect: classify each aspect it sends as "+
				"preserved (declared in the spec, so it is read and written with its version) "+
				"or ownedWhole, with the reason nothing else can be lost.", name, pos)
		}
	}
	for name := range aspectWriters {
		if _, ok := found[name]; !ok {
			t.Errorf("aspectWriters classifies %s, which no longer calls writeAspects; remove the entry", name)
		}
	}
}

// unmanagedSentinel is a field no DataHub aspect defines, standing in for every
// field the provider does not model.
const unmanagedSentinel = "tfAspectWriteGuardSentinel"

// recordedPost is one aspect of one POST the guard server received.
type recordedPost struct {
	URN    string
	Aspect string
	Raw    json.RawMessage
	Value  map[string]any
	// Headers is the per-aspect "headers" object, nil when absent.
	Headers map[string]string
	// HasSystemMetadata reports whether the aspect echoed a systemMetadata
	// object, which a writer must never do: the server keeps a sent runId,
	// which would tie the provider's write to an ingestion run.
	HasSystemMetadata bool
	// Status is what the guard answered.
	Status int
}

// recordedGet is one entity GET the guard server received.
type recordedGet struct {
	Aspects        []string
	SystemMetadata bool
}

// aspectGuard is the server every aspect writer is run against. It behaves
// like DataHub for the parts of the OpenAPI v3 surface the writers use: GET
// serves each stored aspect, with systemMetadata.version when asked; POST
// checks each aspect's If-Version-Match against the stored version, answers
// 412 in the server's shape and stores nothing from the batch on a mismatch,
// and otherwise stores every aspect and advances its version. An aspect
// nothing has written yet holds only unmanagedSentinel, at a version seeded
// distinct from every other aspect's, so a writer that sends a constant or
// cross-wires the versions of a two-aspect POST is caught.
type aspectGuard struct {
	*httptest.Server
	mu       sync.Mutex
	versions map[string]int
	written  map[string]json.RawMessage
	// absent aspects are left out of GET bodies (404 when nothing else was
	// asked for) and expected at version "-1" on POST.
	absent map[string]bool
	// noVersion serves aspects without systemMetadata, as a server that
	// predates aspect versions would.
	noVersion bool
	// bumpOnGet maps an aspect to how many more times its version advances,
	// and unmanagedConcurrentEdit is set, after a GET that serves it.
	bumpOnGet map[string]int
	// always, when non-zero, is the status every POST receives, with
	// alwaysBody as its body when set and retryAfter as its Retry-After.
	always     int
	alwaysBody string
	retryAfter string
	// failGetOnce, when non-zero, is the status the next GET with
	// systemMetadata receives instead of an answer.
	failGetOnce int
	posts       []recordedPost
	gets        []recordedGet
}

// unmanagedConcurrentEdit is the field a simulated concurrent writer adds.
const unmanagedConcurrentEdit = "tfConcurrentEditSentinel"

func newAspectGuard(t *testing.T) *aspectGuard {
	t.Helper()
	g := &aspectGuard{
		versions:  map[string]int{},
		written:   map[string]json.RawMessage{},
		absent:    map[string]bool{},
		bumpOnGet: map[string]int{},
	}
	g.Server = httptest.NewServer(http.HandlerFunc(g.serve))
	t.Cleanup(g.Close)
	return g
}

// seededVersion gives each aspect a distinct, non-trivial version the first
// time it is asked about.
func (g *aspectGuard) seededVersion(aspect string) int {
	if v, ok := g.versions[aspect]; ok {
		return v
	}
	v := 7 + 2*len(g.versions)
	g.versions[aspect] = v
	return v
}

func (g *aspectGuard) version(aspect string) string {
	if g.absent[aspect] {
		return "-1"
	}
	return strconv.Itoa(g.seededVersion(aspect))
}

func (g *aspectGuard) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		g.serveGet(w, r)
	case http.MethodPost:
		g.servePost(w, r)
	default:
		_, _ = w.Write([]byte("[]"))
	}
}

func (g *aspectGuard) serveGet(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	withMeta := r.URL.Query().Get("systemMetadata") == "true"
	var asked []string
	for _, aspect := range strings.Split(r.URL.Query().Get("aspects"), ",") {
		if aspect != "" {
			asked = append(asked, aspect)
		}
	}
	g.gets = append(g.gets, recordedGet{Aspects: asked, SystemMetadata: withMeta})
	if withMeta && g.failGetOnce != 0 {
		status := g.failGetOnce
		g.failGetOnce = 0
		if status == http.StatusServiceUnavailable || status == http.StatusTooManyRequests {
			w.Header().Set("Retry-After", "0")
		}
		http.Error(w, "guard: injected read failure", status)
		return
	}
	entity := map[string]any{"urn": "urn:li:guard:entity"}
	if len(asked) == 0 {
		for aspect := range g.written {
			asked = append(asked, aspect)
		}
	}
	served := 0
	for _, aspect := range asked {
		if g.absent[aspect] {
			continue
		}
		a := map[string]any{}
		if raw, ok := g.written[aspect]; ok {
			_ = json.Unmarshal(raw, &a)
			delete(a, "headers")
		} else {
			a["value"] = map[string]any{unmanagedSentinel: "keep-me"}
		}
		if withMeta && !g.noVersion {
			a["systemMetadata"] = map[string]any{"version": g.version(aspect)}
		}
		entity[aspect] = a
		served++
	}
	if served == 0 && len(asked) > 0 {
		http.NotFound(w, r)
		return
	}
	for _, aspect := range asked {
		if g.bumpOnGet[aspect] > 0 {
			g.bumpOnGet[aspect]--
			g.versions[aspect] = g.seededVersion(aspect) + 1
			g.absent[aspect] = false
			var a map[string]any
			if raw, ok := g.written[aspect]; ok {
				_ = json.Unmarshal(raw, &a)
			} else {
				a = map[string]any{"value": map[string]any{unmanagedSentinel: "keep-me"}}
			}
			value, _ := a["value"].(map[string]any)
			value[unmanagedConcurrentEdit] = "theirs"
			a["value"] = value
			delete(a, "headers")
			raw, _ := json.Marshal(a)
			g.written[aspect] = raw
		}
	}
	_ = json.NewEncoder(w).Encode(entity)
}

func (g *aspectGuard) servePost(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("async") != "false" {
		http.Error(w, `{"error":"guard: every aspect POST must carry async=false, or a precondition failure arrives on a Kafka topic instead of this response"}`, http.StatusBadRequest)
		return
	}
	var body []map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "decoding POST: "+err.Error(), http.StatusBadRequest)
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	var batch []recordedPost
	status := http.StatusOK
	var failure string
	for _, entity := range body {
		var urn string
		_ = json.Unmarshal(entity["urn"], &urn)
		for aspect, raw := range entity {
			if aspect == "urn" {
				continue
			}
			var a struct {
				Value          map[string]any    `json:"value"`
				Headers        map[string]string `json:"headers"`
				SystemMetadata json.RawMessage   `json:"systemMetadata"`
			}
			_ = json.Unmarshal(raw, &a)
			rec := recordedPost{URN: urn, Aspect: aspect, Raw: raw, Value: a.Value, Headers: a.Headers, HasSystemMetadata: len(a.SystemMetadata) > 0}
			if want, ok := a.Headers["If-Version-Match"]; ok && status == http.StatusOK {
				if actual := g.version(aspect); want != actual {
					status = http.StatusPreconditionFailed
					failure = fmt.Sprintf("Failed to validate MCP due to: ValidationExceptionCollection{EntityAspect:(%s,%s) Exceptions: "+
						"[AspectValidationException(item=ChangeItemImpl{changeType=UPSERT}, changeType=UPSERT, entityUrn=%s, aspectName=%s, "+
						"subType=PRECONDITION, msg=Expected version %s, actual version %s)]}", urn, aspect, urn, aspect, want, actual)
				}
			}
			batch = append(batch, rec)
		}
	}
	if g.always != 0 {
		status = g.always
		failure = "forced by the guard"
		if g.alwaysBody != "" {
			failure = g.alwaysBody
		}
	}
	for i := range batch {
		batch[i].Status = status
	}
	g.posts = append(g.posts, batch...)
	switch {
	case status == http.StatusPreconditionFailed:
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Precondition Error", "message": failure})
		return
	case status >= http.StatusBadRequest:
		if status == http.StatusServiceUnavailable || status == http.StatusTooManyRequests {
			retryAfter := g.retryAfter
			if retryAfter == "" {
				retryAfter = "0"
			}
			w.Header().Set("Retry-After", retryAfter)
		}
		w.WriteHeader(status)
		if g.alwaysBody != "" {
			_, _ = w.Write([]byte(failure))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"error": failure})
		return
	}
	for _, rec := range batch {
		g.written[rec.Aspect] = rec.Raw
		g.versions[rec.Aspect] = g.seededVersion(rec.Aspect) + 1
		g.absent[rec.Aspect] = false
	}
	_, _ = w.Write([]byte("[]"))
}

// sent returns the last recorded POST of each aspect.
func (g *aspectGuard) sent() map[string]recordedPost {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := map[string]recordedPost{}
	for _, p := range g.posts {
		out[p.Aspect] = p
	}
	return out
}

func (g *aspectGuard) readsOf(aspect string) []recordedGet {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []recordedGet
	for _, get := range g.gets {
		for _, a := range get.Aspects {
			if a == aspect {
				out = append(out, get)
			}
		}
	}
	return out
}

func sortedWriterNames() []string {
	names := make([]string, 0, len(aspectWriters))
	for name := range aspectWriters {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func TestOpenAPIAspectWritesKeepUnmanagedFields(t *testing.T) {
	for _, name := range sortedWriterNames() {
		w := aspectWriters[name]
		t.Run(name, func(t *testing.T) {
			for aspect, rule := range w.aspects {
				if rule.ownership == ownWholeAspect && strings.TrimSpace(rule.reason) == "" {
					t.Errorf("%s is classified ownedWhole without a reason", aspect)
				}
			}

			g := newAspectGuard(t)
			if err := w.write(t.Context(), newTestClient(t, g.Server)); err != nil {
				t.Fatalf("writer failed: %v", err)
			}

			sent := g.sent()
			for aspect, p := range sent {
				if _, ok := w.aspects[aspect]; !ok {
					t.Errorf("sent aspect %q, which aspectWriters does not classify for %s", aspect, name)
				}
				// A typed aspect struct that already wraps its value, handed
				// to the body builder, arrives as {"value":{"value":...}}.
				// DataHub Cloud rejects it ("field is required but not
				// found"); OSS 1.7 derives key aspects from the URN and let
				// it through, which is how it reached a live run unseen.
				if len(p.Value) == 0 {
					t.Errorf("%s was sent with an empty value: %s", aspect, p.Raw)
				}
				if inner, ok := p.Value["value"].(map[string]any); ok && len(p.Value) == 1 {
					t.Errorf("%s is double-wrapped: the aspect's value is itself {\"value\": %v}; pass the record, not the aspect envelope", aspect, inner)
				}
			}
			for aspect, rule := range w.aspects {
				p, ok := sent[aspect]
				if !ok {
					t.Errorf("classified aspect %q was never sent; update the invocation or the classification", aspect)
					continue
				}
				if rule.ownership != preserveUnmanaged {
					continue
				}
				if p.Value[unmanagedSentinel] != "keep-me" {
					t.Errorf("%s is classified preserved, but the write dropped a field the provider does not "+
						"manage: the stored aspect carried %q and the POST sent %s", aspect, unmanagedSentinel, p.Raw)
				}
			}
		})
	}
}

// TestOpenAPIAspectWritesCarryVersionPrecondition checks that every preserved
// aspect is written with If-Version-Match equal to the version the writer
// read, and nothing else is: an owned-whole or key aspect carries no
// precondition, and no aspect echoes the systemMetadata it read. A preserved
// write without the version it read can overwrite a change made between its
// read and its write, with HTTP 200 and no trace.
func TestOpenAPIAspectWritesCarryVersionPrecondition(t *testing.T) {
	for _, name := range sortedWriterNames() {
		w := aspectWriters[name]
		t.Run(name, func(t *testing.T) {
			g := newAspectGuard(t)
			// The versions the reads will serve; the successful write then
			// advances them, so they are captured first.
			served := map[string]string{}
			g.mu.Lock()
			for aspect := range w.aspects {
				served[aspect] = strconv.Itoa(g.seededVersion(aspect))
			}
			g.mu.Unlock()
			if err := w.write(t.Context(), newTestClient(t, g.Server)); err != nil {
				t.Fatalf("writer failed: %v", err)
			}
			for aspect, p := range g.sent() {
				if p.HasSystemMetadata {
					t.Errorf("%s echoed systemMetadata back on the write; the server keeps a sent runId, which "+
						"would tie the provider's write to an ingestion run and revert it with that run", aspect)
				}
				rule, ok := w.aspects[aspect]
				if !ok {
					continue
				}
				header, present := p.Headers["If-Version-Match"]
				if rule.ownership != preserveUnmanaged {
					if present {
						t.Errorf("%s is owned whole but carried If-Version-Match %q; only aspects the writer read carry a precondition", aspect, header)
					}
					continue
				}
				want := served[aspect]
				if !present {
					t.Errorf("%s is preserved but the POST carried no If-Version-Match: sent %s", aspect, p.Raw)
					continue
				}
				if header != want {
					t.Errorf("%s carried If-Version-Match %q but the read served version %q", aspect, header, want)
				}
				read := false
				for _, get := range g.readsOf(aspect) {
					if get.SystemMetadata {
						read = true
					}
				}
				if !read {
					t.Errorf("%s was written with a version but never read with systemMetadata=true; the version did not come from the aspect", aspect)
				}
			}
		})
	}
}

// TestOpenAPIAspectWritesSendAbsentSentinel makes each preserved aspect absent
// in turn and checks the writer creates it with If-Version-Match "-1", which
// the server reads as "must not exist yet": two creators racing for one URN
// then produce one success and one 412, never two overwrites.
func TestOpenAPIAspectWritesSendAbsentSentinel(t *testing.T) {
	for _, name := range sortedWriterNames() {
		w := aspectWriters[name]
		for aspect, rule := range w.aspects {
			if rule.ownership != preserveUnmanaged {
				continue
			}
			t.Run(name+"/"+aspect, func(t *testing.T) {
				g := newAspectGuard(t)
				g.absent[aspect] = true
				if err := w.write(t.Context(), newTestClient(t, g.Server)); err != nil {
					t.Fatalf("writer failed against an absent %s: %v", aspect, err)
				}
				p, ok := g.sent()[aspect]
				if !ok {
					t.Fatalf("%s was never sent", aspect)
				}
				if got := p.Headers["If-Version-Match"]; got != "-1" {
					t.Errorf("creating absent %s sent If-Version-Match %q, want \"-1\"", aspect, got)
				}
			})
		}
	}
}

// TestOpenAPIAspectWritesStopOnFailedRead fails the read of each preserved
// aspect in turn and checks the writer returns an error without sending that
// aspect. A writer that carried on after a failed read would POST only the
// fields it manages, which is the wipe the read exists to prevent.
func TestOpenAPIAspectWritesStopOnFailedRead(t *testing.T) {
	for _, name := range sortedWriterNames() {
		w := aspectWriters[name]
		for aspect, rule := range w.aspects {
			if rule.ownership != preserveUnmanaged {
				continue
			}
			t.Run(name+"/"+aspect+"/read_500", func(t *testing.T) {
				g := newAspectGuard(t)
				server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet && strings.Contains(r.URL.Query().Get("aspects"), aspect) {
						http.Error(rw, "injected read failure", http.StatusInternalServerError)
						return
					}
					g.serve(rw, r)
				}))
				defer server.Close()

				if err := w.write(t.Context(), newTestClient(t, server)); err == nil {
					t.Errorf("reading %s failed with HTTP 500, but the writer returned no error", aspect)
				}
				if _, ok := g.sent()[aspect]; ok {
					t.Errorf("reading %s failed, but the writer still sent it", aspect)
				}
			})
			// A server that serves the aspect without systemMetadata.version
			// cannot be written to conditionally. The writer must refuse rather
			// than fall back to an unconditional write.
			t.Run(name+"/"+aspect+"/no_version", func(t *testing.T) {
				g := newAspectGuard(t)
				g.noVersion = true
				err := w.write(t.Context(), newTestClient(t, g.Server))
				var unavailable *AspectVersionUnavailableError
				if !errors.As(err, &unavailable) {
					t.Errorf("the read of %s carried no systemMetadata.version; want *AspectVersionUnavailableError, got %v", aspect, err)
				} else if !strings.Contains(err.Error(), "systemMetadata.version") || !strings.Contains(err.Error(), "did not write it") {
					t.Errorf("the refusal should say what was missing and that nothing was written: %v", err)
				}
				if _, ok := g.sent()[aspect]; ok {
					t.Errorf("the read of %s carried no version, but the writer still sent it", aspect)
				}
			})
		}
	}
}
