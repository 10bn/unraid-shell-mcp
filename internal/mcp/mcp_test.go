package mcp

import (
	"context"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/10bn/unraid-shell-mcp/internal/whitelist"
)

func resultText(t *testing.T, res *mcpsdk.CallToolResult) string {
	t.Helper()
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(mcpsdk.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

func callExecute(t *testing.T, matcher *whitelist.Matcher, command string) *mcpsdk.CallToolResult {
	t.Helper()
	handler := makeExecuteHandler(matcher)
	req := mcpsdk.CallToolRequest{}
	req.Params.Name = toolName
	req.Params.Arguments = map[string]any{"command": command}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	return res
}

func TestExecuteCommandRejectsWhenNotWhitelisted(t *testing.T) {
	matcher, err := whitelist.New(nil, nil)
	if err != nil {
		t.Fatalf("whitelist.New: %v", err)
	}
	res := callExecute(t, matcher, "echo hello")
	if !res.IsError {
		t.Fatalf("expected error result for non-whitelisted command, got: %s", resultText(t, res))
	}
}

func TestExecuteCommandRunsWhitelistedCommand(t *testing.T) {
	matcher, err := whitelist.New([]string{`^echo\b`}, nil)
	if err != nil {
		t.Fatalf("whitelist.New: %v", err)
	}
	res := callExecute(t, matcher, "echo hello-world")
	if res.IsError {
		t.Fatalf("expected success, got error: %s", resultText(t, res))
	}
	text := resultText(t, res)
	if !strings.Contains(text, "exit code: 0") || !strings.Contains(text, "hello-world") {
		t.Fatalf("unexpected result text: %s", text)
	}
}

func TestExecuteCommandHardBlocklistWinsOverPermissiveWhitelist(t *testing.T) {
	matcher, err := whitelist.New([]string{`.*`}, nil)
	if err != nil {
		t.Fatalf("whitelist.New: %v", err)
	}
	res := callExecute(t, matcher, "rm -rf /")
	if !res.IsError {
		t.Fatalf("expected hard blocklist to reject command, got: %s", resultText(t, res))
	}
}
