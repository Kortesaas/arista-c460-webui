package main

import (
	"slices"
	"testing"
)

func TestDocumentedDiagnosticToolsMatchRuntime(t *testing.T) {
	schema := requestSchema("POST /api/diagnostics")
	properties := schema["properties"].(map[string]any)
	tools := properties["tool"].(map[string]any)["enum"].([]string)
	for _, tool := range []string{"ping", "dns", "trace", "tcp"} {
		if !slices.Contains(tools, tool) {
			t.Fatalf("supported diagnostic %q is missing from the specification", tool)
		}
		input := DiagnosticInput{Tool: tool, Target: "127.0.0.1"}
		if tool == "tcp" {
			input.Port = 80
		}
		if _, err := diagnosticArgs(input); err != nil {
			t.Fatalf("documented diagnostic %q is rejected: %v", tool, err)
		}
	}
}
