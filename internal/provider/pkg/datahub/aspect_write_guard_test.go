// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
	"encoding/json"
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
// preserved.

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
			})
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

func TestEveryOpenAPIAspectWriteIsClassified(t *testing.T) {
	found := openAPIAspectWriteFuncs(t)
	if len(found) == 0 {
		t.Fatal("found no OpenAPI aspect writes at all; the source scan is broken, not the package clean")
	}
	for name, pos := range found {
		if _, ok := aspectWriters[name]; !ok {
			t.Errorf("%s (%s) POSTs to the OpenAPI v3 entity endpoint but is not in aspectWriters. "+
				"An aspect write replaces the whole stored aspect: classify each aspect it sends as "+
				"preserved (read it with unmanagedAspectFields and replace only the managed fields) "+
				"or ownedWhole, with the reason nothing else can be lost.", name, pos)
		}
	}
	for name := range aspectWriters {
		if _, ok := found[name]; !ok {
			t.Errorf("aspectWriters classifies %s, which no longer POSTs to the OpenAPI v3 entity "+
				"endpoint; remove the entry", name)
		}
	}
}

// unmanagedSentinel is a field no DataHub aspect defines, standing in for every
// field the provider does not model.
const unmanagedSentinel = "tfAspectWriteGuardSentinel"

// aspectWriteGuardServer records the body of every POST and serves each
// written aspect back on GET, as DataHub does, so writers that read their write
// back to confirm it are satisfied. An aspect not yet written holds only
// unmanagedSentinel. Anything else gets an empty JSON array, which every writer
// accepts as a successful write.
func aspectWriteGuardServer(t *testing.T, posts *[][]map[string]json.RawMessage) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	written := map[string]json.RawMessage{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			mu.Lock()
			entity := map[string]any{}
			if r.URL.Query().Get("aspects") == "" {
				// A whole-entity read returns everything written so far.
				for aspect, raw := range written {
					entity[aspect] = raw
				}
			}
			for _, aspect := range strings.Split(r.URL.Query().Get("aspects"), ",") {
				if aspect == "" {
					continue
				}
				if raw, ok := written[aspect]; ok {
					entity[aspect] = raw
				} else {
					entity[aspect] = map[string]any{"value": map[string]any{unmanagedSentinel: "keep-me"}}
				}
			}
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(entity)
		case http.MethodPost:
			var body []map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decoding POST %s: %v", r.URL.Path, err)
			}
			mu.Lock()
			*posts = append(*posts, body)
			for _, entity := range body {
				for aspect, raw := range entity {
					if aspect != "urn" {
						written[aspect] = raw
					}
				}
			}
			mu.Unlock()
			_, _ = w.Write([]byte("[]"))
		default:
			_, _ = w.Write([]byte("[]"))
		}
	}))
}

func TestOpenAPIAspectWritesKeepUnmanagedFields(t *testing.T) {
	names := make([]string, 0, len(aspectWriters))
	for name := range aspectWriters {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		w := aspectWriters[name]
		t.Run(name, func(t *testing.T) {
			for aspect, rule := range w.aspects {
				if rule.ownership == ownWholeAspect && strings.TrimSpace(rule.reason) == "" {
					t.Errorf("%s is classified ownedWhole without a reason", aspect)
				}
			}

			var posts [][]map[string]json.RawMessage
			server := aspectWriteGuardServer(t, &posts)
			defer server.Close()
			if err := w.write(t.Context(), newTestClient(t, server)); err != nil {
				t.Fatalf("writer failed: %v", err)
			}

			sent := map[string]json.RawMessage{}
			for _, body := range posts {
				for _, entity := range body {
					for aspect, raw := range entity {
						if aspect != "urn" {
							sent[aspect] = raw
						}
					}
				}
			}
			for aspect := range sent {
				if _, ok := w.aspects[aspect]; !ok {
					t.Errorf("sent aspect %q, which aspectWriters does not classify for %s", aspect, name)
				}
			}
			for aspect, rule := range w.aspects {
				raw, ok := sent[aspect]
				if !ok {
					t.Errorf("classified aspect %q was never sent; update the invocation or the classification", aspect)
					continue
				}
				if rule.ownership != preserveUnmanaged {
					continue
				}
				var a struct {
					Value map[string]any `json:"value"`
				}
				if err := json.Unmarshal(raw, &a); err != nil {
					t.Errorf("decoding sent %s: %v", aspect, err)
					continue
				}
				if a.Value[unmanagedSentinel] != "keep-me" {
					t.Errorf("%s is classified preserved, but the write dropped a field the provider does not "+
						"manage: the stored aspect carried %q and the POST sent %s", aspect, unmanagedSentinel, raw)
				}
			}
		})
	}
}
