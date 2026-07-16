package allocator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"go.uber.org/zap"
)

// defaultCpsatVenvPython is where the repo's setup instructions put the
// pyallocator interpreter (see pyallocator/README.md).
const defaultCpsatVenvPython = "pyallocator/.venv/bin/python"

// ResolvePythonInterpreter picks the Python interpreter for the CP-SAT
// subprocess: --python flag > ILFORD_CPSAT_PYTHON env > the pyallocator
// venv if present > python3 on PATH.
func ResolvePythonInterpreter(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if envValue := os.Getenv("ILFORD_CPSAT_PYTHON"); envValue != "" {
		return envValue
	}
	if _, err := os.Stat(defaultCpsatVenvPython); err == nil {
		return defaultCpsatVenvPython
	}
	return "python3"
}

// RunCpsatAllocator invokes `<python> -m pyallocator` with the problem on
// stdin and parses the rota from stdout. A non-zero exit means invalid
// input or a crash; INFEASIBLE comes back as exit 0 with Success=false.
func RunCpsatAllocator(ctx context.Context, pythonPath string, input *CpsatInput, logger *zap.Logger) (*CpsatOutput, error) {
	payload, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal cpsat input: %w", err)
	}

	cmd := exec.CommandContext(ctx, pythonPath, "-m", "pyallocator")
	cmd.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	logger.Debug("Running CP-SAT allocator subprocess",
		zap.String("python", pythonPath),
		zap.Int("input_bytes", len(payload)))

	runErr := cmd.Run()
	if stderr.Len() > 0 {
		logger.Debug("pyallocator stderr", zap.String("stderr", stderr.String()))
	}
	if runErr != nil {
		return nil, fmt.Errorf("pyallocator subprocess failed (%s -m pyallocator): %w; stderr: %s",
			pythonPath, runErr, stderrTail(stderr.String()))
	}

	var output CpsatOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		return nil, fmt.Errorf("failed to parse pyallocator output: %w", err)
	}
	if output.Error != "" {
		return nil, fmt.Errorf("pyallocator reported error: %s", output.Error)
	}
	return &output, nil
}

// stderrTail keeps error messages readable if the subprocess dumped a
// long traceback.
func stderrTail(stderr string) string {
	const maxLen = 500
	if len(stderr) <= maxLen {
		return stderr
	}
	return "..." + stderr[len(stderr)-maxLen:]
}
