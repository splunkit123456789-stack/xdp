package mvp

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"xdp/pkg/plugin"
	"xdp/pkg/search/splquery"
)

type executableSearchCommandRequest struct {
	Command       executableSearchCommandInputCommand `json:"command"`
	Input         executableSearchCommandInputRows    `json:"input"`
	RuntimeConfig map[string]any                      `json:"runtime_config,omitempty"`
}

type executableSearchCommandInputCommand struct {
	Name string   `json:"name"`
	Args []string `json:"args"`
	Raw  string   `json:"raw"`
}

type executableSearchCommandInputRows struct {
	Rows   []map[string]any `json:"rows"`
	Fields []string         `json:"fields"`
}

type executableSearchCommandResponse struct {
	Rows       []map[string]any `json:"rows"`
	Fields     []string         `json:"fields"`
	OutputMode string           `json:"output_mode"`
	Error      string           `json:"error,omitempty"`
}

const (
	defaultExecutablePluginTimeoutMS      = 5000
	minExecutablePluginTimeoutMS          = 100
	maxExecutablePluginTimeoutMS          = 30000
	defaultExecutablePluginMaxInputRows   = 10000
	defaultExecutablePluginMaxOutputBytes = 4 << 20
	defaultExecutablePluginInterpreter    = "python3"
	executablePluginRunnerName            = ".xdp_plugin_runner.py"
)

const (
	pluginExecRuntimeNotReadyCode  = "PLUGIN_EXEC_RUNTIME_NOT_READY"
	pluginExecInterpreterDenied    = "PLUGIN_EXEC_INTERPRETER_DENIED"
	pluginExecInputLimitCode       = "PLUGIN_EXEC_INPUT_LIMIT"
	pluginExecTimeoutCode          = "PLUGIN_EXEC_TIMEOUT"
	pluginExecOutputLimitCode      = "PLUGIN_EXEC_OUTPUT_LIMIT"
	pluginExecPermissionDeniedCode = "PLUGIN_EXEC_PERMISSION_DENIED"
	pluginExecScriptErrorCode      = "PLUGIN_EXEC_SCRIPT_ERROR"
)

type searchCommandPluginExecutionError struct {
	code    string
	message string
}

func (e searchCommandPluginExecutionError) Error() string { return e.message }

func newSearchCommandPluginExecutionError(code, message string) error {
	return searchCommandPluginExecutionError{code: code, message: message}
}

func searchCommandPluginExecutionErrorCode(err error) (string, bool) {
	var execErr searchCommandPluginExecutionError
	if errors.As(err, &execErr) {
		return execErr.code, true
	}
	return "", false
}

type executableSearchCommandAudit struct {
	RuntimeConfig map[string]any
	InputRows     int
	OutputRows    int
	ElapsedMS     int
	StdoutBytes   int
	StderrBytes   int
	Success       bool
	ErrorCode     string
	ErrorMessage  string
}

func executeSearchCommandPluginRuntime(ctx context.Context, input plugin.SearchCommandResult, item PluginImportResponse, command splquery.Command) (plugin.SearchCommandResult, error) {
	switch strings.TrimSpace(item.Runtime) {
	case "executable_search_command":
		return executeExecutableSearchCommand(ctx, input, item, command)
	case "declarative_search_command":
		return executeDeclarativeSearchCommand(input, item, command)
	default:
		return plugin.SearchCommandResult{}, fmt.Errorf("unsupported search command runtime %s", item.Runtime)
	}
}

func executeExecutableSearchCommand(ctx context.Context, input plugin.SearchCommandResult, item PluginImportResponse, command splquery.Command) (plugin.SearchCommandResult, error) {
	result, _, err := executeExecutableSearchCommandMeasured(ctx, input, item, command)
	return result, err
}

func executeExecutableSearchCommandMeasured(ctx context.Context, input plugin.SearchCommandResult, item PluginImportResponse, command splquery.Command) (plugin.SearchCommandResult, executableSearchCommandAudit, error) {
	started := time.Now()
	audit := executableSearchCommandAudit{
		RuntimeConfig: effectiveExecutablePluginRuntimeConfig(item.RuntimeConfig),
		InputRows:     len(input.Rows),
	}
	fail := func(code string, message string) (plugin.SearchCommandResult, executableSearchCommandAudit, error) {
		audit.ElapsedMS = int(time.Since(started).Milliseconds())
		audit.Success = false
		audit.ErrorCode = code
		audit.ErrorMessage = message
		return plugin.SearchCommandResult{}, audit, newSearchCommandPluginExecutionError(code, message)
	}
	entrypoint := cleanPluginEntrypoint(item.Entrypoint)
	if entrypoint == "" {
		return fail(pluginExecRuntimeNotReadyCode, fmt.Sprintf("search command plugin %s missing entrypoint", item.PluginCode))
	}
	workdir, err := executablePluginRuntimeDir(item)
	if err != nil {
		return fail(pluginExecRuntimeNotReadyCode, err.Error())
	}
	executablePath := filepath.Join(workdir, filepath.FromSlash(entrypoint))
	if _, err := os.Stat(executablePath); err != nil {
		return fail(pluginExecRuntimeNotReadyCode, fmt.Sprintf("search command plugin %s runtime is not prepared, enable the plugin again", item.PluginCode))
	}
	if len(input.Rows) > executablePluginMaxInputRows(item.RuntimeConfig) {
		return fail(pluginExecInputLimitCode, fmt.Sprintf("search command plugin %s input rows exceed limit", item.PluginCode))
	}

	timeout := executablePluginTimeout(item.RuntimeConfig)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	interpreter, err := executablePluginInterpreter(item.RuntimeConfig)
	if err != nil {
		return fail(pluginExecInterpreterDenied, fmt.Sprintf("search command plugin %s invalid interpreter: %s", item.PluginCode, err.Error()))
	}
	var cmd *exec.Cmd
	if interpreter != "" {
		runnerPath := filepath.Join(workdir, executablePluginRunnerName)
		cmd = exec.CommandContext(runCtx, interpreter, "-I", runnerPath, executablePath)
	} else {
		cmd = exec.CommandContext(runCtx, executablePath)
	}
	cmd.Dir = workdir
	cmd.Env = []string{
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"PYTHONNOUSERSITE=1",
		"PYTHONDONTWRITEBYTECODE=1",
		"XDP_PLUGIN_RUNTIME_DIR=" + workdir,
	}
	payload, err := json.Marshal(executableSearchCommandRequest{
		Command: executableSearchCommandInputCommand{Name: command.Name, Args: command.Args, Raw: command.Raw},
		Input: executableSearchCommandInputRows{
			Rows:   input.Rows,
			Fields: input.Fields,
		},
		RuntimeConfig: item.RuntimeConfig,
	})
	if err != nil {
		return fail(pluginExecScriptErrorCode, fmt.Sprintf("encode search command request failed: %s", err.Error()))
	}
	cmd.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	maxOutputBytes := int64(executablePluginMaxOutputBytes(item.RuntimeConfig))
	cmd.Stdout = &limitedBufferWriter{buffer: &stdout, limit: maxOutputBytes}
	cmd.Stderr = &limitedBufferWriter{buffer: &stderr, limit: maxOutputBytes}
	if err := cmd.Run(); err != nil {
		audit.StdoutBytes = stdout.Len()
		audit.StderrBytes = stderr.Len()
		if runCtx.Err() == context.DeadlineExceeded {
			return fail(pluginExecTimeoutCode, fmt.Sprintf("search command plugin %s timed out", item.PluginCode))
		}
		if isLimitedBufferError(err) {
			return fail(pluginExecOutputLimitCode, fmt.Sprintf("search command plugin %s output exceeds limit", item.PluginCode))
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		code := pluginExecScriptErrorCode
		if isExecutablePluginPermissionError(message) {
			code = pluginExecPermissionDeniedCode
		}
		return fail(code, fmt.Sprintf("search command plugin %s failed: %s", item.PluginCode, message))
	}
	audit.StdoutBytes = stdout.Len()
	audit.StderrBytes = stderr.Len()
	if stdout.Len() > executablePluginMaxOutputBytes(item.RuntimeConfig) || stderr.Len() > executablePluginMaxOutputBytes(item.RuntimeConfig) {
		return fail(pluginExecOutputLimitCode, fmt.Sprintf("search command plugin %s output exceeds limit", item.PluginCode))
	}
	var response executableSearchCommandResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		return fail(pluginExecScriptErrorCode, fmt.Sprintf("search command plugin %s returned invalid json: %s", item.PluginCode, err.Error()))
	}
	if strings.TrimSpace(response.Error) != "" {
		return fail(pluginExecScriptErrorCode, fmt.Sprintf("search command plugin %s failed: %s", item.PluginCode, response.Error))
	}
	if response.Rows == nil {
		response.Rows = []map[string]any{}
	}
	audit.ElapsedMS = int(time.Since(started).Milliseconds())
	audit.Success = true
	audit.OutputRows = len(response.Rows)
	return plugin.SearchCommandResult{Rows: response.Rows, Fields: response.Fields, OutputMode: response.OutputMode}, audit, nil
}

func prepareExecutableSearchCommandPlugin(item PluginImportResponse) error {
	if strings.TrimSpace(item.Runtime) != "executable_search_command" {
		return nil
	}
	entrypoint := cleanPluginEntrypoint(item.Entrypoint)
	if entrypoint == "" {
		return fmt.Errorf("search command plugin %s missing entrypoint", item.PluginCode)
	}
	if len(item.PackageBytes) == 0 {
		return fmt.Errorf("search command plugin %s missing executable package", item.PluginCode)
	}
	runtimeDir, err := executablePluginRuntimeDir(item)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(runtimeDir); err != nil {
		return fmt.Errorf("clean search command plugin runtime failed: %w", err)
	}
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		return fmt.Errorf("prepare search command plugin runtime failed: %w", err)
	}
	if err := extractPluginPackage(item.PackageBytes, runtimeDir); err != nil {
		return err
	}
	if err := writeExecutablePluginRunner(runtimeDir); err != nil {
		return err
	}
	executablePath := filepath.Join(runtimeDir, filepath.FromSlash(entrypoint))
	if _, err := os.Stat(executablePath); err != nil {
		return fmt.Errorf("search command plugin %s entrypoint not found after prepare", item.PluginCode)
	}
	return nil
}

func writeExecutablePluginRunner(runtimeDir string) error {
	runnerPath := filepath.Join(runtimeDir, executablePluginRunnerName)
	if err := os.WriteFile(runnerPath, []byte(executablePluginRunnerSource), 0o700); err != nil {
		return fmt.Errorf("write executable plugin runner failed: %w", err)
	}
	return nil
}

func removeExecutableSearchCommandPluginRuntime(item PluginImportResponse) error {
	if strings.TrimSpace(item.Runtime) != "executable_search_command" {
		return nil
	}
	runtimeDir, err := executablePluginRuntimeDir(item)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(runtimeDir); err != nil {
		return fmt.Errorf("remove search command plugin runtime failed: %w", err)
	}
	return nil
}

func removeExecutableSearchCommandPluginCodeRuntime(item PluginImportResponse) error {
	if strings.TrimSpace(item.Runtime) != "executable_search_command" {
		return nil
	}
	base, err := executablePluginRuntimeBaseDir()
	if err != nil {
		return err
	}
	codeDir := filepath.Join(base, sanitizeRuntimePathSegment(item.PluginType), sanitizeRuntimePathSegment(item.PluginCode))
	if err := os.RemoveAll(codeDir); err != nil {
		return fmt.Errorf("remove search command plugin runtime failed: %w", err)
	}
	return nil
}

func executablePluginRuntimeDir(item PluginImportResponse) (string, error) {
	if item.PluginType == "" || item.PluginCode == "" || item.Checksum == "" {
		return "", fmt.Errorf("search command plugin %s runtime metadata is incomplete", item.PluginCode)
	}
	base, err := executablePluginRuntimeBaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(
		base,
		sanitizeRuntimePathSegment(item.PluginType),
		sanitizeRuntimePathSegment(item.PluginCode),
		sanitizeRuntimePathSegment(item.Checksum),
	), nil
}

func executablePluginRuntimeBaseDir() (string, error) {
	root := strings.TrimSpace(os.Getenv("XDP_PLUGIN_RUNTIME_DIR"))
	if root == "" {
		root = filepath.Join(os.TempDir(), "xdp-plugin-runtime")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("prepare plugin runtime base failed: %w", err)
	}
	return root, nil
}

func sanitizeRuntimePathSegment(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "sha256:")
	if value == "" {
		return "_"
	}
	var builder strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			builder.WriteRune(r)
			continue
		}
		builder.WriteByte('_')
	}
	cleaned := strings.Trim(builder.String(), ".")
	if cleaned == "" {
		return "_"
	}
	return cleaned
}

func extractPluginPackage(data []byte, targetDir string) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("plugin package must be a zip file")
	}
	for _, file := range zr.File {
		cleanName := cleanZipPath(file.Name)
		if cleanName == "" {
			return fmt.Errorf("plugin package contains unsafe path %s", file.Name)
		}
		targetPath := filepath.Join(targetDir, filepath.FromSlash(cleanName))
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		rc, err := file.Open()
		if err != nil {
			return err
		}
		content, readErr := io.ReadAll(rc)
		closeErr := rc.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		mode := file.Mode()
		if mode == 0 {
			mode = 0o644
		}
		if err := os.WriteFile(targetPath, content, mode|0o600); err != nil {
			return err
		}
	}
	return nil
}

func executablePluginTimeout(config map[string]any) time.Duration {
	return time.Duration(executablePluginTimeoutMS(config)) * time.Millisecond
}

func executablePluginTimeoutMS(config map[string]any) int {
	timeoutMS := defaultExecutablePluginTimeoutMS
	switch value := config["timeout_ms"].(type) {
	case float64:
		timeoutMS = int(value)
	case int:
		timeoutMS = value
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
			timeoutMS = parsed
		}
	}
	if timeoutMS < minExecutablePluginTimeoutMS {
		timeoutMS = minExecutablePluginTimeoutMS
	}
	if timeoutMS > maxExecutablePluginTimeoutMS {
		timeoutMS = maxExecutablePluginTimeoutMS
	}
	return timeoutMS
}

const executablePluginRunnerSource = `import os
import runpy
import sys

ROOT = os.path.realpath(os.environ.get("XDP_PLUGIN_RUNTIME_DIR", os.getcwd()))
READ_ONLY_ROOTS = {
    os.path.realpath(path)
    for path in (
        sys.base_prefix,
        sys.prefix,
        sys.base_exec_prefix,
        sys.exec_prefix,
    )
    if path
}
for path in sys.path:
    if path:
        real = os.path.realpath(path)
        if real.startswith(tuple(READ_ONLY_ROOTS)):
            READ_ONLY_ROOTS.add(real)

def _inside_runtime(path):
    if not path:
        return True
    if isinstance(path, int):
        return True
    path = os.fspath(path)
    if path.startswith("<") and path.endswith(">"):
        return True
    if not os.path.isabs(path):
        path = os.path.join(ROOT, path)
    real = os.path.realpath(path)
    return real == ROOT or real.startswith(ROOT + os.sep)

def _inside_readonly_root(path):
    if not path or isinstance(path, int):
        return False
    path = os.fspath(path)
    if path.startswith("<") and path.endswith(">"):
        return False
    if not os.path.isabs(path):
        path = os.path.join(ROOT, path)
    real = os.path.realpath(path)
    for root in READ_ONLY_ROOTS:
        if real == root or real.startswith(root + os.sep):
            return True
    return False

def _first_path(args):
    if not args:
        return None
    return args[0]

def _is_read_open(args):
    if len(args) < 2:
        return True
    mode = args[1]
    if isinstance(mode, str):
        return not any(flag in mode for flag in ("w", "a", "x", "+"))
    if len(args) >= 3 and isinstance(args[2], int):
        flags = args[2]
        write_flags = (
            getattr(os, "O_WRONLY", 0)
            | getattr(os, "O_RDWR", 0)
            | getattr(os, "O_CREAT", 0)
            | getattr(os, "O_TRUNC", 0)
            | getattr(os, "O_APPEND", 0)
        )
        return flags & write_flags == 0
    return True

def _audit(event, args):
    if event in {"open", "os.open"}:
        path = _first_path(args)
        if path is not None and not _inside_runtime(path):
            if _is_read_open(args) and _inside_readonly_root(path):
                return
            raise PermissionError(f"access outside plugin runtime dir is blocked: {path}")
    if event in {"os.listdir", "os.scandir", "os.stat"}:
        path = _first_path(args)
        if path is not None and not _inside_runtime(path) and not _inside_readonly_root(path):
            raise PermissionError(f"access outside plugin runtime dir is blocked: {path}")
    if event in {
        "os.chdir",
        "os.chmod",
        "os.chown",
        "os.link",
        "os.mkdir",
        "os.remove",
        "os.rename",
        "os.rmdir",
        "os.symlink",
        "shutil.copyfile",
        "shutil.copymode",
        "shutil.copystat",
        "shutil.copytree",
        "shutil.move",
    }:
        path = _first_path(args)
        if path is not None and not _inside_runtime(path):
            raise PermissionError(f"write outside plugin runtime dir is blocked: {path}")
    if event in {"subprocess.Popen", "socket.connect", "socket.bind"}:
        raise PermissionError(f"{event} is blocked for executable search command plugins")

sys.addaudithook(_audit)
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)

if len(sys.argv) != 2:
    raise SystemExit("entrypoint argument is required")

entrypoint = os.path.realpath(sys.argv[1])
if not _inside_runtime(entrypoint):
    raise PermissionError("entrypoint must be inside plugin runtime dir")

sys.argv = [entrypoint]
runpy.run_path(entrypoint, run_name="__main__")
`

func executablePluginInterpreter(config map[string]any) (string, error) {
	interpreter := strings.TrimSpace(textFromMap(config, "interpreter"))
	if interpreter == "" {
		interpreter = defaultExecutablePluginInterpreter
	}
	if strings.Contains(interpreter, "/") || strings.Contains(interpreter, "\\") {
		return "", fmt.Errorf("absolute or relative interpreter paths are not allowed")
	}
	switch interpreter {
	case "python3":
		return interpreter, nil
	default:
		return "", fmt.Errorf("only python3 is allowed")
	}
}

func effectiveExecutablePluginRuntimeConfig(config map[string]any) map[string]any {
	interpreter, err := executablePluginInterpreter(config)
	if err != nil {
		interpreter = strings.TrimSpace(textFromMap(config, "interpreter"))
	}
	return map[string]any{
		"interpreter":      interpreter,
		"timeout_ms":       executablePluginTimeoutMS(config),
		"max_input_rows":   executablePluginMaxInputRows(config),
		"max_output_bytes": executablePluginMaxOutputBytes(config),
	}
}

func executablePluginMaxInputRows(config map[string]any) int {
	return boundedIntFromMap(config, "max_input_rows", defaultExecutablePluginMaxInputRows, 1, 100000)
}

func executablePluginMaxOutputBytes(config map[string]any) int {
	return boundedIntFromMap(config, "max_output_bytes", defaultExecutablePluginMaxOutputBytes, 1024, 16<<20)
}

func boundedIntFromMap(config map[string]any, key string, fallback, minValue, maxValue int) int {
	value := fallback
	switch v := config[key].(type) {
	case float64:
		if !math.IsNaN(v) && !math.IsInf(v, 0) {
			value = int(v)
		}
	case int:
		value = v
	case int64:
		value = int(v)
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			value = parsed
		}
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

type limitedBufferWriter struct {
	buffer *bytes.Buffer
	limit  int64
}

func (w *limitedBufferWriter) Write(p []byte) (int, error) {
	if w == nil || w.buffer == nil {
		return len(p), nil
	}
	if int64(w.buffer.Len()+len(p)) > w.limit {
		remaining := int(w.limit) - w.buffer.Len()
		if remaining > 0 {
			_, _ = w.buffer.Write(p[:remaining])
		}
		return 0, errLimitedBuffer
	}
	return w.buffer.Write(p)
}

var errLimitedBuffer = fmt.Errorf("output exceeds limit")

func isLimitedBufferError(err error) bool {
	if err == nil {
		return false
	}
	if err == errLimitedBuffer {
		return true
	}
	return strings.Contains(err.Error(), errLimitedBuffer.Error())
}

func isExecutablePluginPermissionError(message string) bool {
	message = strings.ToLower(message)
	return strings.Contains(message, "permissionerror") ||
		strings.Contains(message, "blocked") ||
		strings.Contains(message, "outside plugin runtime dir")
}
