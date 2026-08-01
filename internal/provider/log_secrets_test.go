// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// secretLogFieldKeys are field keys a tflog call must never carry, because the
// value behind each is either a credential or a blob that routinely contains
// one. Matched exactly, so the safe "_len" forms this repository uses instead
// (payload_len, body_len) are unaffected.
var secretLogFieldKeys = map[string]string{
	"api_key":       "an API key",
	"apikey":        "an API key",
	"auth":          "a credential",
	"authorization": "the Authorization header value",
	"body":          "a response body, which echoes back whatever was stored",
	"cookie":        "a session credential",
	"credential":    "a credential",
	"credentials":   "a credential",
	"header":        "request headers, which carry Authorization",
	"headers":       "request headers, which carry Authorization",
	"password":      "a password",
	"passwd":        "a password",
	"payload":       "a request body, which carries whatever was submitted",
	"private_key":   "a private key",
	"secret":        "a secret",
	"token":         "a token",
}

// logFuncPackages are the logging packages whose calls are checked.
var logFuncPackages = map[string]bool{"tflog": true, "tfsdklog": true}

// TestNoSecretsInLogFields asserts that no tflog call passes a field key whose
// value would be a credential.
//
// The framework's Sensitive and WriteOnly markers do not reach this code path.
// They govern what Terraform does with a value -- state serialization, plan
// rendering, diagnostics -- and tflog is a separate channel that writes exactly
// the map it is handed. So an attribute can carry every protection the schema
// offers and still be logged in clear text once the provider marshals it into a
// request and logs the request.
//
// That is a real bug this repository shipped: the signUp debug log carried the
// user's password, the invite token, and the Authorization bearer credential,
// from 0.4.0 through 0.19.0. It survived sixteen releases precisely because
// initial_password is declared Sensitive and WriteOnly, so the value looked
// protected everywhere a reader would think to check.
//
// A structural test rather than a runtime one: the hazard is a field key in a
// map literal, it costs one test to check every call site in the module at
// once, and it covers logging added later with no further work. Log the length
// or a boolean instead of the value -- a field that is never populated cannot
// be un-redacted by a later edit the way a mask can be bypassed.
func TestNoSecretsInLogFields(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving repository root: %v", err)
	}

	var files []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "bin", "dist", "vendor":
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	if len(files) == 0 {
		t.Fatal("no .go files found; is this test running from internal/provider?")
	}

	var calls int
	for _, file := range files {
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}

		rel, err := filepath.Rel(root, file)
		if err != nil {
			rel = file
		}

		ast.Inspect(parsed, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || !logFuncPackages[pkg.Name] {
				return true
			}
			calls++

			for _, arg := range call.Args {
				lit, ok := arg.(*ast.CompositeLit)
				if !ok {
					continue
				}
				for _, elt := range lit.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, ok := kv.Key.(*ast.BasicLit)
					if !ok || key.Kind != token.STRING {
						continue
					}
					name := strings.ToLower(strings.Trim(key.Value, `"`))
					reason, banned := secretLogFieldKeys[name]
					if !banned {
						continue
					}
					t.Errorf("%s:%d: %s.%s logs field %q, which holds %s.\n"+
						"Log a length or a boolean instead (e.g. %q), never the value itself.",
						rel, fset.Position(kv.Pos()).Line, pkg.Name, sel.Sel.Name,
						name, reason, name+"_len")
				}
			}
			return true
		})
	}

	if calls == 0 {
		t.Fatal("no tflog/tfsdklog calls found; the walk or the matcher is broken, " +
			"so this test would pass no matter what was logged")
	}
	t.Logf("checked %d tflog/tfsdklog calls across %d files", calls, len(files))
}
