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

// TestDefaultTagsVerifiedBeforeAnyWrite asserts, for every resource that
// supports provider-level default tags, that its Create verifies the tags
// before making its first server write.
//
// Verifying afterwards orphans an entity: Create writes it, the tag check
// fails, Create returns an error without setting state, and Terraform is left
// with no state entry while the entity survives on the server, invisible to
// both plan and state. That is a real bug this repository shipped and had to
// fix across ten resources at once.
//
// This is a structural test rather than ten near-identical acceptance tests
// because the hazard is a misplaced line in replicated wiring. It checks all
// resources exhaustively, costs one test to run, and - unlike a runtime test
// per resource - automatically covers any tag-capable resource added later.
func TestDefaultTagsVerifiedBeforeAnyWrite(t *testing.T) {
	t.Parallel()

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
				switch f := call.Fun.(type) {
				case *ast.Ident:
					if f.Name == "resolveAndVerifyTags" && verifyLine == 0 {
						verifyLine = fset.Position(call.Pos()).Line
					}
				case *ast.SelectorExpr:
					// Match r.client.<Something> - the provider's server writes.
					inner, ok := f.X.(*ast.SelectorExpr)
					if !ok || inner.Sel.Name != "client" {
						return true
					}
					if recv, ok := inner.X.(*ast.Ident); !ok || recv.Name != "r" {
						return true
					}
					name := f.Sel.Name
					isWrite := strings.HasPrefix(name, "Create") ||
						strings.HasPrefix(name, "Upsert") ||
						strings.HasPrefix(name, "Set") ||
						strings.HasPrefix(name, "Update") ||
						strings.HasPrefix(name, "Add")
					if isWrite && writeLine == 0 {
						writeLine = fset.Position(call.Pos()).Line
					}
				}
				return true
			})

			// Only resources wired for default tags are in scope.
			if verifyLine == 0 {
				continue
			}
			checked++

			if writeLine != 0 && verifyLine > writeLine {
				t.Errorf("%s: Create calls resolveAndVerifyTags at line %d, after its first "+
					"server write at line %d. A nonexistent tag would leave the entity created "+
					"and orphaned (Create errors without setting state, so destroy cannot remove "+
					"it). Move the verification above the first write.",
					file, verifyLine, writeLine)
			}
		}
	}

	// Guard against the test silently passing because the helper was renamed or
	// the wiring removed wholesale.
	const wantAtLeast = 10
	if checked < wantAtLeast {
		t.Errorf("only %d Create functions call resolveAndVerifyTags; expected at least %d "+
			"(corp_user, corp_group, service_account, data_product and the six assertion "+
			"resources). Was the helper renamed, or default-tag wiring dropped from a resource?",
			checked, wantAtLeast)
	}
}
