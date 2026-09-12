package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestRejectAliasedOutputsBeforeWriting(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	alias := filepath.Join(dir, "alias")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	scene := filepath.Join(real, "scene.json")
	data, err := os.ReadFile("../../examples/hello.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scene, data, 0o600); err != nil {
		t.Fatal(err)
	}
	// A report through a directory alias must not replace the input scene.
	args := []string{"render", "--scene", scene, "--out", filepath.Join(dir, "out.png"), "--report", filepath.Join(alias, "scene.json")}
	if err := Run(context.Background(), args, bytes.NewReader(nil), io.Discard, io.Discard); err == nil {
		t.Fatal("accepted aliased input/report")
	}
	got, err := os.ReadFile(scene)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("input scene changed", err)
	}
	// Neither output exists yet, and both include a not-yet-created directory.
	if err := distinctPaths(scene, filepath.Join(real, "new", "same"), filepath.Join(alias, "new", "same")); err == nil {
		t.Fatal("accepted aliased new outputs")
	}
	hardlink := filepath.Join(dir, "hardlink")
	if err := os.Link(scene, hardlink); err != nil {
		t.Fatal(err)
	}
	if err := distinctPaths(scene, "", hardlink); err == nil {
		t.Fatal("accepted hardlink alias")
	}
	for _, args := range [][3]string{{scene, "-", ""}, {scene, "", "-"}} {
		if err := distinctPaths(args[0], args[1], args[2]); err == nil {
			t.Fatal("treated output - as stdin")
		}
	}
	if err := distinctPaths("-", filepath.Join(dir, "ok.png"), filepath.Join(dir, "ok.json")); err != nil {
		t.Fatal(err)
	}
}
