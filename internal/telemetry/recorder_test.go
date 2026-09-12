package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecorderRotationAndAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs/events.jsonl")
	r, err := Open(path, 512)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		if err := r.Record(Event{Schema: SchemaV1, Type: DaemonStarted, RunID: "test", At: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(path + "*")
	if err != nil || len(paths) != 4 {
		t.Fatal(paths, err)
	}
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil || st.Size() > 512 || st.Mode().Perm() != 0o600 {
			t.Fatal(st, err)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			var e Event
			if err := json.Unmarshal([]byte(line), &e); err != nil {
				t.Fatal(err)
			}
		}
	}
	r, err = Open(path, 512)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.Record(Event{Schema: SchemaV1, Type: DaemonStarted, RunID: "second", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
}
func TestRecorderRefusesTruncatedTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	os.WriteFile(path, []byte(`{"schema":`), 0o600)
	if r, err := Open(path, 512); err == nil {
		r.Close()
		t.Fatal("appended to incomplete JSONL")
	}
}
func TestRunIDsAndRawStatus(t *testing.T) {
	a, err := NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewRunID()
	if a == b || len(a) != 32 {
		t.Fatal(a, b)
	}
	s := StatusFromTuples(3, map[uint32]uint32{0x0a: 80, 0x20: 0xfffffff4, 0x22: 4000})
	if s.BatteryPercent.Value != 80 || s.BatteryCurrent.Signed != -12 || s.BatteryVoltageMV.Value != 4000 {
		t.Fatal(s)
	}
}
