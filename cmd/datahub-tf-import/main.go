// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

// datahub-tf-import generates Terraform import configuration for existing
// DataHub resources, allowing brownfield deployments to be adopted into
// Terraform state without manual HCL authoring.
//
// Usage:
//
//	datahub-tf-import enumerate [flags]
//	datahub-tf-import enumerate --output ./import
//	datahub-tf-import enumerate --output ./import --types datahub_secret,datahub_connection
//
// The enumerate command:
//  1. Enumerates all DataHub resources of each registered type.
//  2. Writes import.tf with one import block per resource.
//  3. Runs terraform plan -generate-config-out to generate resource blocks.
//  4. Post-processes the generated config (WriteOnly attribute stubs, platform hints).
//  5. Writes variables.tf for any sensitive values and IMPORT_README.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	_ "github.com/datahub-project/terraform-provider-datahub/cmd/datahub-tf-import/internal/reg"
	"github.com/datahub-project/terraform-provider-datahub/internal/importtool"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "enumerate":
		runEnumerate(os.Args[2:])
	case "version", "--version", "-version":
		fmt.Printf("datahub-tf-import %s\n", version)
	case "help", "--help", "-help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func runEnumerate(args []string) {
	fs := flag.NewFlagSet("enumerate", flag.ExitOnError)
	output := fs.String("output", ".", "Directory to write generated files into")
	types := fs.String("types", "", "Comma-separated resource types to include (default: all)")
	skipTF := fs.Bool("skip-terraform", false, "Write import.tf only; skip terraform init/plan steps")
	skipVal := fs.Bool("skip-validation", false, "Skip final terraform plan validation")
	_ = fs.Parse(args)

	opts := importtool.Options{
		OutputDir:       *output,
		Types:           *types,
		ProviderVersion: version,
		SkipTerraform:   *skipTF,
		SkipValidation:  *skipVal,
	}

	if err := importtool.Run(context.Background(), opts); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `datahub-tf-import -- Terraform import helper for DataHub

Reads DATAHUB_GMS_URL and DATAHUB_GMS_TOKEN from the environment.

Commands:
  enumerate   Enumerate DataHub resources and generate Terraform import config

enumerate flags:
  --output string         Output directory (default ".")
  --types string          Comma-separated types to include (default: all)
  --skip-terraform        Write import.tf only; skip terraform init/plan
  --skip-validation       Skip final terraform plan validation

Example:
  datahub-tf-import enumerate --output ./import
  datahub-tf-import enumerate --output ./import --types datahub_secret,datahub_connection
`)
}
