package skill

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/autocarto/carto_agent/internal/compute"
)

type ComputeInvoker interface {
	Invoke(ctx context.Context, req compute.InvokeRequest) (*compute.InvokeResponse, error)
}

type CLIBackend struct {
	client ComputeInvoker
}

type ComputeBackend = CLIBackend

func NewCLIBackend(client ComputeInvoker) *CLIBackend {
	return &CLIBackend{client: client}
}

func NewComputeBackend(client ComputeInvoker) *CLIBackend {
	return NewCLIBackend(client)
}

func (b *CLIBackend) ID() string {
	return RuntimeIDCLI
}

func (b *CLIBackend) Execute(ctx context.Context, toolName string, args map[string]any) (*BackendResult, error) {
	definition := SkillDefinitionFromContext(ctx)
	if definition != nil && definition.Execution.CLI != nil {
		return b.executeCustomCLI(ctx, definition, args)
	}
	if b == nil || b.client == nil {
		return nil, fmt.Errorf("cli backend client is nil")
	}

	params := cloneMapAny(args)
	if params == nil {
		params = make(map[string]any)
	}
	projectID := SessionContextFromContext(ctx).ProjectID
	if rawProjectID, ok := params["project_id"].(string); ok && strings.TrimSpace(rawProjectID) != "" {
		projectID = strings.TrimSpace(rawProjectID)
	}
	delete(params, "project_id")

	resp, err := b.client.Invoke(ctx, compute.InvokeRequest{
		ProjectID: projectID,
		Tool:      strings.TrimSpace(toolName),
		Params:    params,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("cli backend returned nil response")
	}

	return &BackendResult{
		Success:      resp.Success,
		Output:       cloneAny(resp.Output),
		Error:        strings.TrimSpace(resp.Error),
		OutputLayers: toBackendOutputLayersFromCompute(resp.OutputLayers),
	}, nil
}

func (b *CLIBackend) executeCustomCLI(ctx context.Context, definition *SkillDefinition, args map[string]any) (*BackendResult, error) {
	if definition == nil || definition.Execution.CLI == nil {
		return nil, fmt.Errorf("cli backend definition is missing cli config")
	}
	cli := definition.Execution.CLI
	session := SessionContextFromContext(ctx)
	env := resolveEnv{vars: buildResolveVars(args, session, definition.SourcePath), session: session}

	commandValue, err := resolveTemplateValue(cli.Command, env)
	if err != nil {
		return nil, fmt.Errorf("resolve cli command: %w", err)
	}
	command := strings.TrimSpace(stringifyResolvedValue(commandValue))
	if command == "" {
		return nil, fmt.Errorf("cli command is empty")
	}

	cmdArgs := make([]string, 0, len(cli.Args))
	for _, rawArg := range cli.Args {
		resolved, resolveErr := resolveTemplateValue(rawArg, env)
		if resolveErr != nil {
			return nil, fmt.Errorf("resolve cli arg %q: %w", rawArg, resolveErr)
		}
		cmdArgs = append(cmdArgs, stringifyResolvedValue(resolved))
	}

	runCtx := ctx
	var cancel context.CancelFunc
	if timeoutText := strings.TrimSpace(cli.Timeout); timeoutText != "" {
		timeout, parseErr := time.ParseDuration(timeoutText)
		if parseErr != nil {
			return nil, fmt.Errorf("parse cli timeout %q: %w", timeoutText, parseErr)
		}
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(runCtx, command, cmdArgs...)
	cmd.WaitDelay = 2 * time.Second
	if workingDir := strings.TrimSpace(cli.WorkingDir); workingDir != "" {
		resolvedWorkingDir, resolveErr := resolveTemplateValue(workingDir, env)
		if resolveErr != nil {
			return nil, fmt.Errorf("resolve cli working_dir: %w", resolveErr)
		}
		cmd.Dir = strings.TrimSpace(stringifyResolvedValue(resolvedWorkingDir))
	}
	cmd.Env = buildCLIEnv(cli.Env, env)
	if stdinMode := strings.TrimSpace(cli.Stdin); stdinMode == "" || strings.EqualFold(stdinMode, "json") {
		payload, marshalErr := json.Marshal(args)
		if marshalErr != nil {
			return nil, fmt.Errorf("marshal cli stdin: %w", marshalErr)
		}
		cmd.Stdin = bytes.NewReader(payload)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		trimmedStdout := strings.TrimSpace(stdout.String())
		trimmedStderr := strings.TrimSpace(stderr.String())
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("cli command failed: exit=%d stderr=%s stdout=%s", exitErr.ExitCode(), trimmedStderr, trimmedStdout)
		}
		return nil, fmt.Errorf("cli command failed: %v stderr=%s stdout=%s", err, trimmedStderr, trimmedStdout)
	}

	return parseCLIBackendResult(cli.Stdout, stdout.Bytes())
}

func buildCLIEnv(rawEnv map[string]string, env resolveEnv) []string {
	out := append([]string(nil), os.Environ()...)
	if len(rawEnv) == 0 {
		return out
	}
	keys := make([]string, 0, len(rawEnv))
	for key := range rawEnv {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		resolved, err := resolveTemplateValue(rawEnv[key], env)
		if err != nil {
			continue
		}
		out = append(out, fmt.Sprintf("%s=%s", key, stringifyResolvedValue(resolved)))
	}
	return out
}

func parseCLIBackendResult(stdoutMode string, stdout []byte) (*BackendResult, error) {
	mode := strings.ToLower(strings.TrimSpace(stdoutMode))
	if mode == "" || mode == "json" {
		var result BackendResult
		if err := json.Unmarshal(stdout, &result); err == nil && (result.Success || result.Error != "" || result.Output != nil || len(result.OutputLayers) > 0) {
			return &result, nil
		}
		var generic any
		if err := json.Unmarshal(stdout, &generic); err != nil {
			return nil, fmt.Errorf("decode cli json output failed: %w", err)
		}
		return &BackendResult{Success: true, Output: generic}, nil
	}
	return &BackendResult{Success: true, Output: strings.TrimSpace(string(stdout))}, nil
}

func toBackendOutputLayersFromCompute(src []compute.OutputLayer) []OutputLayer {
	if len(src) == 0 {
		return nil
	}
	out := make([]OutputLayer, 0, len(src))
	for _, layer := range src {
		out = append(out, OutputLayer{Path: strings.TrimSpace(layer.Path), AutoLoad: layer.AutoLoad})
	}
	return out
}
