// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

// datahub-tf-export exports existing DataHub resources to Terraform configuration,
// allowing brownfield deployments to be adopted into Terraform state without
// manual HCL authoring.
//
// Usage:
//
//	datahub-tf-export enumerate [flags]
//	datahub-tf-export enumerate --output ./export
//	datahub-tf-export enumerate --output ./export --types datahub_secret,datahub_connection
//
// The enumerate command:
//  1. Enumerates all DataHub resources of each registered type.
//  2. Writes import.tf with one import block per resource.
//  3. Runs terraform plan -generate-config-out to generate resource blocks.
//  4. Post-processes the generated config (WriteOnly attribute stubs, platform hints).
//  5. Writes variables.tf for any sensitive values and IMPORT_README.md.
//  6. Run terraform apply on the output directory to import the resources into state.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	_ "github.com/datahub-project/terraform-provider-datahub/cmd/datahub-tf-export/internal/reg"
	"github.com/datahub-project/terraform-provider-datahub/internal/exporttool"
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
		fmt.Printf("datahub-tf-export %s\n", version)
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

	opts := exporttool.Options{
		OutputDir:       *output,
		Types:           *types,
		ProviderVersion: version,
		SkipTerraform:   *skipTF,
		SkipValidation:  *skipVal,
	}

	if err := exporttool.Run(context.Background(), opts); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `datahub-tf-export -- exports DataHub resources to Terraform configuration

Reads DATAHUB_GMS_URL and DATAHUB_GMS_TOKEN from the environment.

Commands:
  enumerate   Enumerate DataHub resources and generate Terraform configuration

enumerate flags:
  --output string         Output directory (default ".")
  --types string          Comma-separated types to include (default: all)
  --skip-terraform        Write import.tf only; skip terraform init/plan
  --skip-validation       Skip final terraform plan validation

Example:
  datahub-tf-export enumerate --output ./export
  datahub-tf-export enumerate --output ./export --types datahub_secret,datahub_connection
`)
}
