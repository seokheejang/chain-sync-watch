package cmdopenapi_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// cswBinary builds the csw binary into a temp dir once per test
// process and returns its path. Running the subcommand via the
// binary (instead of calling cmdopenapi.Run directly) lets us
// verify the stdout redirection and --output file behaviour as a
// real CLI user would.
func cswBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "csw")
	build := exec.Command("go", "build", "-o", bin, "github.com/seokheejang/chain-sync-watch/cmd/csw")
	build.Stderr = os.Stderr
	require.NoError(t, build.Run())
	return bin
}

func TestOpenAPIDump_JSONToStdout(t *testing.T) {
	bin := cswBinary(t)

	var out bytes.Buffer
	cmd := exec.Command(bin, "openapi-dump", "--format=json")
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run())

	var doc map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &doc))
	require.Equal(t, "3.1.0", doc["openapi"])
	info, ok := doc["info"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "chain-sync-watch", info["title"])
}

func TestOpenAPIDump_YAMLToFile(t *testing.T) {
	bin := cswBinary(t)
	outPath := filepath.Join(t.TempDir(), "openapi.yaml")

	cmd := exec.Command(bin, "openapi-dump", "--format=yaml", "--output="+outPath)
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run())

	data, err := os.ReadFile(outPath)
	require.NoError(t, err)
	require.Contains(t, string(data), "openapi: 3.1.0")
	require.Contains(t, string(data), "chain-sync-watch")
}

func TestOpenAPIDump_UnknownFormatFails(t *testing.T) {
	bin := cswBinary(t)
	cmd := exec.Command(bin, "openapi-dump", "--format=xml")
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	require.Error(t, err, "unknown format must fail")
}
