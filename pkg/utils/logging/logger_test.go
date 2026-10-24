package logging

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A hosted environment runs in a container whose working directory the server
// cannot write to, so it must log to stdout and leave no logs directory behind.
func TestInitLogger_HostedEnvironmentsWriteNoLogFile(t *testing.T) {
	for _, env := range []string{"prod", "preprod"} {
		t.Run(env, func(t *testing.T) {
			t.Chdir(t.TempDir())

			logger, err := InitLogger(env)
			require.NoError(t, err)
			logger.Info("hello")

			_, err = os.Stat("logs")
			assert.True(t, os.IsNotExist(err), "want no logs directory, got err=%v", err)
		})
	}
}

func TestInitLogger_LocalEnvironmentsWriteALogFile(t *testing.T) {
	t.Chdir(t.TempDir())

	_, err := InitLogger("test")
	require.NoError(t, err)

	entries, err := os.ReadDir("logs")
	require.NoError(t, err)
	assert.Len(t, entries, 1)
}
