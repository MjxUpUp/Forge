package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/Harness/forge/internal/hooks"
	"github.com/spf13/cobra"
)

var hookCmd = &cobra.Command{
	Use:    "hook <name>",
	Short:  "Run an embedded hook script by name",
	Long:   "Executes the named hook script embedded in the forge binary with CWD set to the project root. Designed to be called from Claude Code hook settings so that hooks work regardless of CWD.",
	Args:   cobra.ExactArgs(1),
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		content, ok := hooks.EmbeddedContent(name)
		if !ok {
			return fmt.Errorf("unknown hook: %s", name)
		}

		// Find project root to set as CWD for the hook script.
		root, err := findProjectRoot()
		if err != nil {
			// Not in a forge project — skip silently (same behavior as the bash hooks).
			return nil
		}

		// Execute the embedded script content via bash with CWD = project root.
		bash, err := exec.LookPath("bash")
		if err != nil {
			return fmt.Errorf("bash not found in PATH: %w", err)
		}

		shCmd := exec.Command(bash, "-s")
		shCmd.Stdin = newStringReader(content)
		shCmd.Dir = root
		shCmd.Stdout = os.Stdout
		shCmd.Stderr = os.Stderr

		return shCmd.Run()
	},
}

// stringReader is a simple io.Reader wrapper for a string.
type stringReader struct {
	s   string
	pos int
}

func newStringReader(s string) *stringReader {
	return &stringReader{s: s}
}

func (r *stringReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.s) {
		return 0, io.EOF
	}
	n = copy(p, r.s[r.pos:])
	r.pos += n
	return
}

func init() {
	rootCmd.AddCommand(hookCmd)
}
