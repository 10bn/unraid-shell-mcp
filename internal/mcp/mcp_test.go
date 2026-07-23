package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/10bn/unraid-shell-mcp/internal/whitelist"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(testWriter{}, nil))
}

// testWriter discards log output so tests don't spam stdout; use t.Log
// instead if a test ever needs to inspect log content.
type testWriter struct{}

func (testWriter) Write(p []byte) (int, error) { return len(p), nil }

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
	return callExecuteWithArgs(t, matcher, map[string]any{"command": command})
}

func callExecuteWithArgs(t *testing.T, matcher *whitelist.Matcher, args map[string]any) *mcpsdk.CallToolResult {
	t.Helper()
	handler := makeExecuteHandler(matcher, testLogger())
	req := mcpsdk.CallToolRequest{}
	req.Params.Name = toolName
	req.Params.Arguments = args
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	return res
}

func TestExecuteCommandRejectsWhenNotWhitelisted(t *testing.T) {
	matcher, err := whitelist.New(nil, nil, false)
	if err != nil {
		t.Fatalf("whitelist.New: %v", err)
	}
	res := callExecute(t, matcher, "echo hello")
	if !res.IsError {
		t.Fatalf("expected error result for non-whitelisted command, got: %s", resultText(t, res))
	}
}

func TestExecuteCommandRunsWhitelistedCommand(t *testing.T) {
	matcher, err := whitelist.New([]string{`^echo\b.*$`}, nil, false)
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
	matcher, err := whitelist.New([]string{`.*`}, nil, false)
	if err != nil {
		t.Fatalf("whitelist.New: %v", err)
	}
	res := callExecute(t, matcher, "rm -rf /")
	if !res.IsError {
		t.Fatalf("expected hard blocklist to reject command, got: %s", resultText(t, res))
	}
}

func TestExecuteCommandInjectionViaPrefixWhitelistIsRejected(t *testing.T) {
	// Regression test for the fix that made whitelist matching full-string:
	// a whitelist entry anchored only at the start must not let a shell
	// metacharacter smuggle in a second command.
	matcher, err := whitelist.New([]string{`^echo\b`}, nil, false)
	if err != nil {
		t.Fatalf("whitelist.New: %v", err)
	}
	res := callExecute(t, matcher, "echo hi; cat /etc/shadow")
	if !res.IsError {
		t.Fatalf("expected injected command to be rejected, got: %s", resultText(t, res))
	}
}

func TestExecuteCommandOutputCapTerminatesRunawayOutput(t *testing.T) {
	matcher, err := whitelist.New([]string{`^yes\b.*$`}, nil, false)
	if err != nil {
		t.Fatalf("whitelist.New: %v", err)
	}
	// "yes" writes an endless stream of "y\n"; without a cap this would run
	// until the (much longer) timeout, consuming unbounded memory.
	res := callExecuteWithArgs(t, matcher, map[string]any{
		"command":        "yes",
		"timeoutSeconds": float64(5),
	})
	if !res.IsError {
		t.Fatalf("expected output-cap termination to report as an error result, got: %s", resultText(t, res))
	}
	text := resultText(t, res)
	if !strings.Contains(text, fmt.Sprintf("%d byte limit", maxOutputBytes)) {
		t.Fatalf("expected output-cap message, got: %s", text)
	}
}
