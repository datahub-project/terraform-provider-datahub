// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// Structural guards for the provider-level defaults mechanisms: every Create
// that consumes a default must verify that default before it makes its first
// server write.
//
// Verifying afterwards orphans an entity: Create writes it, the verification
// fails, Create returns an error without setting state, and Terraform is left
// with no state entry while the entity survives on the server, invisible to
// both plan and state. That is a real bug this repository shipped twice - once
// for defaults.tags across ten resources, once for
// defaults.structured_properties across eight.
//
// These are structural tests rather than eighteen near-identical acceptance
// tests because the hazard is a misplaced line in replicated wiring. They
// check every resource exhaustively, cost one test each to run, and - unlike a
// runtime test per resource - automatically cover any defaults-capable
// resource added later.

// TestDefaultTagsVerifiedBeforeAnyWrite asserts, for every resource that
// supports provider-level default tags, that its Create verifies the tags
// before making its first server write.
func TestDefaultTagsVerifiedBeforeAnyWrite(t *testing.T) {
	t.Parallel()

	assertVerifiedBeforeFirstWrite(t, verifyOrderingCase{
		verifyFunc: "resolveAndVerifyTags",
		hazard: "A nonexistent tag would leave the entity created and orphaned (Create errors " +
			"without setting state, so destroy cannot remove it).",
		wantAtLeast: 10,
		expected: "corp_user, corp_group, service_account, data_product and the six assertion " +
			"resources",
	})
}

// TestDefaultSPDefaultsVerifiedBeforeAnyWrite asserts, for every resource that
// supports provider-level default structured properties, that its Create
// verifies those properties before making its first server write.
func TestDefaultSPDefaultsVerifiedBeforeAnyWrite(t *testing.T) {
	t.Parallel()

	assertVerifiedBeforeFirstWrite(t, verifyOrderingCase{
		verifyFunc: "resolveAndVerifySPDefaults",
		hazard: "A missing property definition, or a default value the definition cannot accept, " +
			"would leave the entity created and orphaned (Create errors without setting state, " +
			"so destroy cannot remove it).",
		wantAtLeast: 8,
		expected: "domain, glossary_node, glossary_term, corp_user, corp_group, service_account, " +
			"data_product and data_contract",
	})
}

// verifyOrderingCase describes one defaults mechanism to check.
type verifyOrderingCase struct {
	// verifyFunc is the resolve-and-verify helper the Create must call. A
	// Create that does not call it is not wired for this mechanism and is
	// skipped.
	verifyFunc string
	// hazard explains, in the failure message, what goes wrong when the order
	// is reversed.
	hazard string
	// wantAtLeast guards against the test silently passing because the helper
	// was renamed or the wiring removed wholesale.
	wantAtLeast int
	// expected names the resources wantAtLeast counts, for that same message.
	expected string
}

// assertVerifiedBeforeFirstWrite parses every resource file and checks the
// ordering inside each Create.
func assertVerifiedBeforeFirstWrite(t *testing.T, tc verifyOrderingCase) {
	t.Helper()

	files, err := filepath.Glob("*_resource.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no *_resource.go files found; is this test running from internal/provider?")
	}

	checked := 0
	for _, file := range files {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}

		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "Create" || fn.Body == nil {
				continue
			}

			verifyLine, writeLine := 0, 0
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch fun := call.Fun.(type) {
				case *ast.Ident:
					if fun.Name == tc.verifyFunc && verifyLine == 0 {
						verifyLine = fset.Position(call.Pos()).Line
					}
				case *ast.SelectorExpr:
					if isServerWrite(fun) && writeLine == 0 {
						writeLine = fset.Position(call.Pos()).Line
					}
				}
				return true
			})

			// Only resources wired for this mechanism are in scope.
			if verifyLine == 0 {
				continue
			}
			checked++

			if writeLine == 0 {
				t.Errorf("%s: Create calls %s but no server write was recognised, so the "+
					"ordering could not be checked. isServerWrite most likely needs to learn "+
					"this resource's write call.", file, tc.verifyFunc)
				continue
			}
			if verifyLine > writeLine {
				t.Errorf("%s: Create calls %s at line %d, after its first server write at "+
					"line %d. %s Move the verification above the first write.",
					file, tc.verifyFunc, verifyLine, writeLine, tc.hazard)
			}
		}
	}

	if checked < tc.wantAtLeast {
		t.Errorf("only %d Create functions call %s; expected at least %d (%s). Was the helper "+
			"renamed, or the wiring dropped from a resource?",
			checked, tc.verifyFunc, tc.wantAtLeast, tc.expected)
	}
}

// isServerWrite reports whether a selector call is one of the resource's own
// server writes: a method reached through the receiver `r` (r.client.X, r.pd.X,
// or an r.x helper of its own) whose name reads as a mutation. Calls rooted
// anywhere else - resp.Diagnostics.AddError, plan.Foo.ValueString - are not
// writes.
func isServerWrite(sel *ast.SelectorExpr) bool {
	root := ast.Expr(sel)
	for {
		inner, ok := root.(*ast.SelectorExpr)
		if !ok {
			break
		}
		root = inner.X
	}
	if ident, ok := root.(*ast.Ident); !ok || ident.Name != "r" {
		return false
	}
	name := strings.ToLower(sel.Sel.Name)
	for _, verb := range []string{"create", "upsert", "set", "update", "add", "write", "delete", "remove"} {
		if strings.HasPrefix(name, verb) {
			return true
		}
	}
	return false
}
