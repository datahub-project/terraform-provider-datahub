// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package importtool

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// terraformInit runs `terraform -chdir=dir init -input=false`.
func terraformInit(ctx context.Context, dir string) error {
	return runTerraform(ctx, dir, false, "init", "-input=false", "-no-color")
}

// terraformGenerateConfig runs `terraform -chdir=dir plan -generate-config-out=generated.tf`.
// It returns (generated bool, err error). generated is true when generated.tf was
// written even if the command exited non-zero (the expected Required+WriteOnly case).
func terraformGenerateConfig(ctx context.Context, dir string) (generated bool, err error) {
	genPath := dir + "/generated.tf"

	cmdErr := runTerraform(ctx, dir, true, "plan",
		"-generate-config-out=generated.tf",
		"-input=false",
		"-no-color",
	)

	_, statErr := os.Stat(genPath)
	generated = statErr == nil

	if cmdErr != nil && !generated {
		return false, fmt.Errorf("terraform plan -generate-config-out failed and no generated.tf was written: %w", cmdErr)
	}
	// Non-zero exit is expected when Required+WriteOnly attrs are present.
	// As long as generated.tf was written we can proceed.
	return generated, nil
}

// terraformPlan runs `terraform -chdir=dir plan -input=false` and returns any error.
func terraformPlan(ctx context.Context, dir string) error {
	return runTerraform(ctx, dir, true, "plan", "-input=false", "-no-color", "-detailed-exitcode")
}

func runTerraform(ctx context.Context, dir string, allowExit2 bool, args ...string) error {
	allArgs := append([]string{"-chdir=" + dir}, args...)
	cmd := exec.CommandContext(ctx, "terraform", allArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// Exit code 2 from `terraform plan -detailed-exitcode` means "changes present".
			// We treat that as a warning, not an error, during the validation step.
			if allowExit2 && exitErr.ExitCode() == 2 {
				return nil
			}
		}
		return fmt.Errorf("terraform %s: %w", args[0], err)
	}
	return nil
}
