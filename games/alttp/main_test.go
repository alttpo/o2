package alttp

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"testing"
)

// TestMain sends the log to a file so that `go test` prints only the test results and failures. The tests log the
// assembly listings of the patch and the test ROM and trace every CPU instruction the emulator runs, which is hundreds
// of MB. The file goes in $O2_TEST_LOG_DIR, or the temp directory when it is not set.
func TestMain(m *testing.M) {
	dir := os.Getenv("O2_TEST_LOG_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	path := filepath.Join(dir, "o2-games-alttp-test.log")

	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not create the test log '%s': %v; logging to stderr\n", path, err)
		os.Exit(m.Run())
	}
	log.SetOutput(f)

	code := m.Run()
	_ = f.Close()
	if code != 0 {
		fmt.Fprintf(os.Stderr, "test log: %s\n", path)
	}
	os.Exit(code)
}
