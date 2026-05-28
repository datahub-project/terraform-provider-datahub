// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package extracttool

import (
	"fmt"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
)

// Variable describes a sensitive Terraform variable that the post-processor
// adds to variables.tf in place of a WriteOnly attribute.
type Variable struct {
	// Name is the variable name used in variables.tf and in the var.X reference.
	Name string
	// Attr is the original attribute name in the resource block.
	Attr string
	// ResourceLabel is the Terraform resource label (e.g. "dbt_cloud_secret").
	ResourceLabel string
	// ResourceType is the Terraform resource type (e.g. "datahub_secret").
	ResourceType string
}

// connectionPlatformStub lines are appended to every datahub_connection block
// to guide the user toward adding the correct platform sub-block. Each non-empty
// line is a comment; empty lines produce blank lines for readability.
var connectionPlatformStubLines = []string{
	"",
	"  # Add ONE of the following platform blocks with your credentials,",
	"  # then run: terraform plan",
	"  #",
	"  # databricks {",
	"  #   workspace_url = \"\"",
	"  #   token         = \"\"",
	"  #   http_path     = \"\"",
	"  #   catalog       = \"\"  # optional",
	"  # }",
	"  # snowflake {",
	"  #   account_id    = \"\"",
	"  #   warehouse     = \"\"",
	"  #   username      = \"\"",
	"  #   password      = \"\"",
	"  # }",
	"  # bigquery {",
	"  #   project       = \"\"",
	"  #   private_key   = \"\"",
	"  # }",
	"  # dataplex {",
	"  #   project       = \"\"",
	"  #   private_key   = \"\"",
	"  # }",
	"  # redshift {",
	"  #   host          = \"\"",
	"  #   port          = 5439",
	"  #   database      = \"\"",
	"  #   username      = \"\"",
	"  #   password      = \"\"",
	"  # }",
	"  # unity_catalog {",
	"  #   workspace_url = \"\"",
	"  #   token         = \"\"",
	"  # }",
	"  # raw_config {",
	"  #   blob = jsonencode({})",
	"  # }",
}

// PostProcess rewrites src (the content of generated.tf) to:
//   - Replace WriteOnly attributes emitted as `null # sensitive` with
//     `var.<label>_<attr>` references and record them for variables.tf.
//   - Set `*_wo_version` and `config_wo_version` trigger attributes to 1.
//   - Append a platform block stub to every datahub_connection block.
//
// It returns the rewritten bytes and the list of variables that must appear
// in variables.tf.
func PostProcess(src []byte) ([]byte, []Variable, error) {
	f, diags := hclwrite.ParseConfig(src, "generated.tf", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, nil, fmt.Errorf("parsing generated.tf: %s", diags.Error())
	}

	var vars []Variable

	for _, block := range f.Body().Blocks() {
		if block.Type() != "resource" {
			continue
		}
		labels := block.Labels()
		if len(labels) < 2 {
			continue
		}
		resourceType := labels[0]
		resourceLabel := labels[1]

		body := block.Body()
		attrs := body.Attributes()

		for attrName, attr := range attrs {
			// Use the full attribute token bytes (includes trailing comment) to detect
			// "null # sensitive" -- the # sensitive comment is not in Expr() tokens.
			rawAttr := string(attr.BuildTokens(nil).Bytes())
			rawExpr := strings.TrimSpace(string(attr.Expr().BuildTokens(nil).Bytes()))

			// WriteOnly attribute emitted as `<attr> = null # sensitive`.
			if rawExpr == "null" && strings.Contains(rawAttr, "sensitive") {
				varName := resourceLabel + "_" + attrName
				body.SetAttributeRaw(attrName, varRefTokens(varName))
				vars = append(vars, Variable{
					Name:          varName,
					Attr:          attrName,
					ResourceLabel: resourceLabel,
					ResourceType:  resourceType,
				})

				// Set the corresponding _wo_version trigger to 1.
				woVersionAttr := attrName + "_wo_version"
				if _, exists := attrs[woVersionAttr]; exists {
					body.SetAttributeValue(woVersionAttr, cty.NumberIntVal(1))
				}
				continue
			}

			// _wo_version or config_wo_version emitted as plain null.
			if (strings.HasSuffix(attrName, "_wo_version") || attrName == "config_wo_version") &&
				rawExpr == "null" {
				body.SetAttributeValue(attrName, cty.NumberIntVal(1))
			}
		}

		// For connection resources: append platform stub comment.
		if resourceType == "datahub_connection" {
			body.AppendUnstructuredTokens(platformStubTokens())
		}
	}

	return f.Bytes(), vars, nil
}

// varRefTokens returns the hclwrite token sequence for a `var.name` expression.
func varRefTokens(varName string) hclwrite.Tokens {
	traversal := hcl.Traversal{
		hcl.TraverseRoot{Name: "var"},
		hcl.TraverseAttr{Name: varName},
	}
	toks := hclwrite.TokensForTraversal(traversal)
	// TokensForTraversal does not add a trailing newline; add one.
	toks = append(toks, &hclwrite.Token{Type: hclsyntax.TokenNewline, Bytes: []byte("\n")})
	return toks
}

// platformStubTokens returns the comment tokens for the connection platform stub.
func platformStubTokens() hclwrite.Tokens {
	var toks hclwrite.Tokens
	for _, line := range connectionPlatformStubLines {
		if line == "" {
			toks = append(toks, &hclwrite.Token{
				Type:  hclsyntax.TokenNewline,
				Bytes: []byte("\n"),
			})
		} else {
			toks = append(toks, &hclwrite.Token{
				Type:  hclsyntax.TokenComment,
				Bytes: []byte(line + "\n"),
			})
		}
	}
	return toks
}
