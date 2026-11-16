package checklog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// writeEntry appends a raw entry to the checklog file with an explicit
// RecordedAt, bypassing Record() (which stamps time.Now()) so filter tests are
// deterministic. Writes to the user-level DataDir (forgedata.DataDirFor), under
// an isolated FORGE_DATA_HOME so the real ~/.forge is never touched.
func writeEntry(t *testing.T, dir string, e Entry) {
	t.Helper()
	isolateDataHome(t)
	path := filepath.Join(forgedata.DataDirFor(dir), "checklog.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	data, _ := json.Marshal(e)
	if _, err := f.Write(append(data, '\n')); err != nil {
		t.Fatal(err)
	}
}
