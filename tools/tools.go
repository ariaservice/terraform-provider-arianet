//go:build tools

// Package tools pins build-time tools in go.mod.
package tools

import (
	_ "github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs"
)
