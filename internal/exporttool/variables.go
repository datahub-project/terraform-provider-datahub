// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package importtool

import (
	"bytes"
	"fmt"
)

// WriteVariablesTF produces the content of variables.tf for the given set
// of sensitive variables. Each variable is declared as sensitive = true so
// that Terraform masks its value in plan output.
func WriteVariablesTF(vars []Variable) []byte {
	if len(vars) == 0 {
		return nil
	}
	var b bytes.Buffer
	for i, v := range vars {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "variable %q {\n", v.Name)
		fmt.Fprintf(&b, "  type      = string\n")
		fmt.Fprintf(&b, "  sensitive = true\n")
		fmt.Fprintf(&b, "  description = %q\n",
			fmt.Sprintf("%s.%s.%s", v.ResourceType, v.ResourceLabel, v.Attr))
		fmt.Fprintf(&b, "}\n")
	}
	return b.Bytes()
}
