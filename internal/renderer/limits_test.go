package renderer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestSceneResourceLimits(t *testing.T) {
	for _, data := range []string{
		`{"version":"lucio.scene/v1","version":"lucio.scene/v1","nodes":[]}`,
		`{"version":"lucio.scene/v1","nodes":[],"canvas":{"width":9223372036854775807}}`,
		`{"version":"lucio.scene/v1","nodes":[{"id":"text","type":"text","box":{"x":0,"y":0,"w":100,"h":100},"text":"a","font":{"size":4096}}]}`,
		strings.Repeat("[", 40) + "0" + strings.Repeat("]", 40),
		string(bytes.Repeat([]byte{' '}, MaxSceneBytes+1)),
		`{"version":null,"nodes":[]}`,
	} {
		_, r := ValidateSceneJSON([]byte(data))
		if !HasErrors(r.Issues) {
			t.Fatalf("accepted input: %.100s", data)
		}
	}
}
func TestParsedScenePreservesHighLevelFields(t *testing.T) {
	data := []byte(`{"version":"lucio.scene/v1","nodes":[{"id":"grid","type":"grid","box":{"x":0,"y":0,"w":100,"h":100},"columns":1,"children":[{"id":"picture","type":"image","src":"photo.png","fit":"contain"}]}]}`)
	scene, r := ValidateSceneJSON(data)
	if !r.OK {
		t.Fatal(r.Issues)
	}
	if scene.Nodes[0].Grid.Children[0].Image.Fit != ImageFitContain {
		t.Fatal("lost nested image semantics")
	}
}
func TestImageSizeBounds(t *testing.T) {
	for _, size := range [][2]int{{0, 1}, {1, 0}, {MaxAssetSide + 1, 1}, {10000, 10000}, {1, int(^uint(0) >> 1)}} {
		if validImageSize(size[0], size[1]) {
			t.Fatal(size)
		}
	}
	if !validImageSize(2880, 5120) {
		t.Fatal("expected a valid high-resolution page")
	}
}
func FuzzScene(f *testing.F) {
	f.Add([]byte(`{"version":"lucio.scene/v1","nodes":[]}`))
	f.Add([]byte(`{"version":"lucio.scene/v1","nodes":[{"id":"r","type":"rect","box":{"x":0,"y":0,"w":100,"h":100},"fill":"#ffffff"}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 16_384 {
			return
		}
		CompileSceneJSON(data)
	})
}

// Every shipped primitive must have both schema and parser support.
func TestSchemaPrimitiveParity(t *testing.T) {
	data, err := SchemaJSON(SceneVersionV1)
	if err != nil {
		t.Fatal(err)
	}
	var schema any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	for _, p := range PrimitiveList() {
		if !bytes.Contains(data, []byte(fmt.Sprintf(`"const": "%s"`, p.Type))) {
			t.Fatalf("schema missing %s", p.Type)
		}
		if allowedAuthorNodeFields(p.Type) == nil {
			t.Fatalf("parser missing %s", p.Type)
		}
	}
}
