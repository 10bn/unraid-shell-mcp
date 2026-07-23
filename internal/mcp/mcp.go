// Package mcp wires the execute-command tool into an MCP server.
package mcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/10bn/unraid-shell-mcp/internal/whitelist"
)

const (
	toolName              = "execute-command"
	defaultTimeoutSeconds = 30
	maxTimeoutSeconds     = 300
)

// New builds an MCP server exposing the execute-command tool. Every
// invocation is checked against matcher before anything runs.
func New(matcher *whitelist.Matcher) *server.MCPServer {
	s := server.NewMCPServer(
		"unraid-shell-mcp",
		"0.1.0",
		server.WithToolCapabilities(false),
	)

	s.AddTool(mcp.NewTool(toolName,
		mcp.WithDescription(
			"Execute a shell command on the Unraid host. The command is checked "+
				"against a hard-coded safety blocklist and the operator-configured "+
				"whitelist/blacklist before it runs; commands that do not match an "+
				"allowed pattern are rejected. Runs full array/Docker/VM access — "+
				"use with caution.",
		),
		mcp.WithString("command",
			mcp.Description("The shell command line to execute (via /bin/sh -c)."),
			mcp.Required(),
		),
		mcp.WithNumber("timeoutSeconds",
			mcp.Description("Maximum time to allow the command to run, in seconds."),
			mcp.DefaultNumber(defaultTimeoutSeconds),
		),
	), makeExecuteHandler(matcher))

	return s
}

func makeExecuteHandler(matcher *whitelist.Matcher) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()

		command, ok := args["command"].(string)
		if !ok || command == "" {
			return mcp.NewToolResultError("missing required string argument: command"), nil
		}

		timeout := defaultTimeoutSeconds
		if raw, present := args["timeoutSeconds"]; present {
			if n, ok := raw.(float64); ok && n > 0 {
				timeout = int(n)
			}
		}
		if timeout > maxTimeoutSeconds {
			timeout = maxTimeoutSeconds
		}

		if allowed, reason := matcher.Allowed(command); !allowed {
			return mcp.NewToolResultError(fmt.Sprintf("command rejected: %s", reason)), nil
		}

		runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()

		var stdout, stderr bytes.Buffer
		cmd := exec.CommandContext(runCtx, "/bin/sh", "-c", command)
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		runErr := cmd.Run()

		exitCode := 0
		var exitErr *exec.ExitError
		switch {
		case errors.As(runErr, &exitErr):
			exitCode = exitErr.ExitCode()
		case errors.Is(runCtx.Err(), context.DeadlineExceeded):
			return mcp.NewToolResultError(fmt.Sprintf("command timed out after %ds", timeout)), nil
		case runErr != nil:
			return mcp.NewToolResultError(fmt.Sprintf("failed to run command: %s", runErr)), nil
		}

		result := fmt.Sprintf(
			"exit code: %d\n--- stdout ---\n%s\n--- stderr ---\n%s",
			exitCode, stdout.String(), stderr.String(),
		)
		return mcp.NewToolResultText(result), nil
	}
}
