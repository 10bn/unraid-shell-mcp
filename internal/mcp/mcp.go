// Package mcp wires the execute-command tool into an MCP server.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/10bn/unraid-shell-mcp/internal/whitelist"
)

const (
	toolName              = "execute-command"
	defaultTimeoutSeconds = 30
	maxTimeoutSeconds     = 300

	// maxOutputBytes caps how much of stdout/stderr each is captured, per
	// stream. A command that exceeds it is terminated rather than left to
	// run to its timeout while writing unbounded output into server memory.
	maxOutputBytes = 1 << 20 // 1 MiB
)

// New builds an MCP server exposing the execute-command tool. Every
// invocation is checked against matcher before anything runs, and every
// invocation (allowed or rejected) is recorded via logger as a structured
// audit event.
func New(matcher *whitelist.Matcher, logger *slog.Logger) *server.MCPServer {
	if logger == nil {
		logger = slog.Default()
	}

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
	), makeExecuteHandler(matcher, logger))

	return s
}

// capturedOutput is an io.Writer that stops growing once it reaches limit
// bytes and cancels the running command instead of letting it keep writing
// unbounded output.
type capturedOutput struct {
	limit     int
	cancel    context.CancelFunc
	buf       []byte
	truncated bool
}

func (w *capturedOutput) Write(p []byte) (int, error) {
	if !w.truncated {
		remaining := w.limit - len(w.buf)
		if remaining > 0 {
			n := len(p)
			if n > remaining {
				n = remaining
			}
			w.buf = append(w.buf, p[:n]...)
		}
		if len(w.buf) >= w.limit {
			w.truncated = true
			w.cancel()
		}
	}
	// Always report the full write as consumed: os/exec expects io.Writer
	// semantics (a short write is treated as an error), and we want the
	// process torn down via the cancelled context, not a broken pipe.
	return len(p), nil
}

func (w *capturedOutput) String() string {
	return string(w.buf)
}

func makeExecuteHandler(matcher *whitelist.Matcher, logger *slog.Logger) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		start := time.Now()
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
			logger.Info("execute-command",
				"outcome", "rejected",
				"command", command,
				"reason", reason,
				"duration_ms", time.Since(start).Milliseconds(),
			)
			return mcp.NewToolResultError(fmt.Sprintf("command rejected: %s", reason)), nil
		}

		runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()

		stdout := &capturedOutput{limit: maxOutputBytes, cancel: cancel}
		stderr := &capturedOutput{limit: maxOutputBytes, cancel: cancel}
		cmd := exec.CommandContext(runCtx, "/bin/sh", "-c", command)
		cmd.Stdout = stdout
		cmd.Stderr = stderr

		// /bin/sh -c may fork children (pipelines, backgrounded commands)
		// rather than exec-replacing itself, so killing just the tracked
		// process on timeout/output-cap can leave grandchildren running
		// and holding the stdout/stderr pipes open forever. Put the whole
		// command in its own process group and kill that group instead.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Cancel = func() error {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		// Safety net: if some descendant still doesn't die (e.g. it
		// re-parented into its own session), don't let Wait block forever
		// waiting for the pipes to reach EOF — force them closed after a
		// short grace period past cancellation.
		cmd.WaitDelay = 5 * time.Second

		runErr := cmd.Run()
		duration := time.Since(start)

		if stdout.truncated || stderr.truncated {
			logger.Info("execute-command",
				"outcome", "output_cap",
				"command", command,
				"duration_ms", duration.Milliseconds(),
			)
			result := fmt.Sprintf(
				"command terminated: output exceeded the %d byte limit per stream\n"+
					"--- stdout (truncated) ---\n%s\n--- stderr (truncated) ---\n%s",
				maxOutputBytes, stdout.String(), stderr.String(),
			)
			return mcp.NewToolResultError(result), nil
		}

		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			logger.Info("execute-command",
				"outcome", "timeout",
				"command", command,
				"timeout_s", timeout,
				"duration_ms", duration.Milliseconds(),
			)
			return mcp.NewToolResultError(fmt.Sprintf("command timed out after %ds", timeout)), nil
		}

		exitCode := 0
		var exitErr *exec.ExitError
		switch {
		case errors.As(runErr, &exitErr):
			exitCode = exitErr.ExitCode()
		case runErr != nil:
			logger.Info("execute-command",
				"outcome", "error",
				"command", command,
				"error", runErr.Error(),
				"duration_ms", duration.Milliseconds(),
			)
			return mcp.NewToolResultError(fmt.Sprintf("failed to run command: %s", runErr)), nil
		}

		logger.Info("execute-command",
			"outcome", "success",
			"command", command,
			"exit_code", exitCode,
			"duration_ms", duration.Milliseconds(),
		)

		result := fmt.Sprintf(
			"exit code: %d\n--- stdout ---\n%s\n--- stderr ---\n%s",
			exitCode, stdout.String(), stderr.String(),
		)
		return mcp.NewToolResultText(result), nil
	}
}
