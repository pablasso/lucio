package renderer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestHexColorToGrayUsesIntegerLuma(t *testing.T) {
	cases := map[string]byte{
		"#000000": 0,
		"#ffffff": 255,
		"#ff0000": 76,
		"#00ff00": 150,
		"#0000ff": 29,
		"#333333": 51,
	}
	for color, want := range cases {
		got, err := HexColorToGray(color)
		if err != nil {
			t.Fatalf("HexColorToGray(%q): %v", color, err)
		}
		if got != want {
			t.Fatalf("HexColorToGray(%q) = %d, want %d", color, got, want)
		}
	}
}

func TestRenderSceneJSONDrawsBackgroundRectAndLine(t *testing.T) {
	result := RenderSceneJSON([]byte(`{
		"version":"lucio.scene/v1",
		"canvas":{"background":"#ffffff"},
		"nodes":[
			{"id":"card","type":"rect","box":{"x":10,"y":20,"w":30,"h":20},"fill":"#333333","stroke":"#000000","stroke_width":2},
			{"id":"rule","type":"line","box":{"x":50,"y":60,"w":20,"h":5},"from":{"x":0,"y":2},"to":{"x":19,"y":2},"stroke":"#000000","stroke_width":3}
		]
	}`))
	if !result.Report.OK {
		t.Fatalf("report OK = false; issues=%+v", result.Report.Issues)
	}
	if len(result.PNG) == 0 {
		t.Fatal("PNG is empty")
	}
	if result.Report.Output.SHA256 == "" {
		t.Fatal("missing output sha256")
	}
	sum := sha256.Sum256(result.PNG)
	if got := result.Report.Output.SHA256; got != hex.EncodeToString(sum[:]) {
		t.Fatalf("sha256 = %s, want %s", got, hex.EncodeToString(sum[:]))
	}
	decoded, err := png.Decode(bytes.NewReader(result.PNG))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != CanvasWidth || decoded.Bounds().Dy() != CanvasHeight {
		t.Fatalf("decoded bounds = %v", decoded.Bounds())
	}
	if got := result.Image.GrayAt(0, 0).Y; got != 255 {
		t.Fatalf("background pixel = %d, want 255", got)
	}
	if got := result.Image.GrayAt(25, 30).Y; got != 51 {
		t.Fatalf("rect fill pixel = %d, want 51", got)
	}
	if got := result.Image.GrayAt(10, 20).Y; got != 0 {
		t.Fatalf("rect stroke pixel = %d, want 0", got)
	}
	for _, y := range []int{61, 62, 63} {
		if got := result.Image.GrayAt(50, y).Y; got != 0 {
			t.Fatalf("line pixel y=%d = %d, want 0", y, got)
		}
	}
}

func TestRenderSceneJSONDrawsTextAndReportsMetrics(t *testing.T) {
	result := RenderSceneJSON([]byte(`{
		"version":"lucio.scene/v1",
		"canvas":{"background":"#ffffff"},
		"nodes":[{"id":"title","type":"text","box":{"x":80,"y":80,"w":500,"h":120},"text":"Hello","font":{"size":64,"weight":"bold"},"align":"center","valign":"middle","color":"#000000"}]
	}`))
	if !result.Report.OK {
		t.Fatalf("report OK = false; issues=%+v", result.Report.Issues)
	}
	if len(result.PNG) == 0 {
		t.Fatal("PNG is empty")
	}
	if len(result.Report.Nodes) != 1 || result.Report.Nodes[0].ActualFontSize == nil || *result.Report.Nodes[0].ActualFontSize != 64 {
		t.Fatalf("missing actual font size in report: %+v", result.Report.Nodes)
	}
	if result.Report.Nodes[0].Lines == nil || *result.Report.Nodes[0].Lines != 1 {
		t.Fatalf("missing line count in report: %+v", result.Report.Nodes)
	}
	if changed := countNonWhite(result.Image, image.Rect(80, 80, 580, 200)); changed == 0 {
		t.Fatal("text box has no drawn pixels")
	}
	if got := result.Image.GrayAt(0, 0).Y; got != 255 {
		t.Fatalf("background pixel = %d, want 255", got)
	}
}

func TestRenderSceneJSONSupportsAllFontAliases(t *testing.T) {
	result := RenderSceneJSON([]byte(`{
		"version":"lucio.scene/v1",
		"nodes":[
			{"id":"sans_regular","type":"text","box":{"x":0,"y":0,"w":300,"h":50},"text":"Sans","font":{"family":"sans","size":24,"weight":"regular"}},
			{"id":"sans_bold","type":"text","box":{"x":0,"y":60,"w":300,"h":50},"text":"Sans bold","font":{"family":"sans","size":24,"weight":"bold"}},
			{"id":"serif_regular","type":"text","box":{"x":0,"y":120,"w":300,"h":50},"text":"Serif","font":{"family":"serif","size":24,"weight":"regular"}},
			{"id":"serif_bold","type":"text","box":{"x":0,"y":180,"w":300,"h":50},"text":"Serif bold","font":{"family":"serif","size":24,"weight":"bold"}},
			{"id":"mono_regular","type":"text","box":{"x":0,"y":240,"w":300,"h":50},"text":"Mono","font":{"family":"mono","size":24,"weight":"regular"}},
			{"id":"mono_bold","type":"text","box":{"x":0,"y":300,"w":300,"h":50},"text":"Mono bold","font":{"family":"mono","size":24,"weight":"bold"}}
		]
	}`))
	if !result.Report.OK {
		t.Fatalf("report OK = false; issues=%+v", result.Report.Issues)
	}
	for i, node := range result.Report.Nodes {
		if node.ActualFontSize == nil || *node.ActualFontSize != 24 || node.Lines == nil || *node.Lines != 1 {
			t.Fatalf("node %d missing metrics: %+v", i, node)
		}
	}
}

func TestRenderSceneJSONReportsTextOverflow(t *testing.T) {
	result := RenderSceneJSON([]byte(`{
		"version":"lucio.scene/v1",
		"nodes":[{"id":"title","type":"text","box":{"x":0,"y":0,"w":40,"h":40},"text":"this line is too long","font":{"size":32}}]
	}`))
	if result.Report.OK {
		t.Fatal("report OK = true, want false")
	}
	if !reportHasIssue(result.Report, "/nodes/0/text", "text_overflow") {
		t.Fatalf("missing text overflow issue: %+v", result.Report.Issues)
	}
	if len(result.PNG) != 0 {
		t.Fatal("PNG should not be emitted when render has errors")
	}
}

func TestRenderSceneJSONWrapsAndShrinksText(t *testing.T) {
	result := RenderSceneJSON([]byte(`{
		"version":"lucio.scene/v1",
		"nodes":[{"id":"body","type":"text","box":{"x":20,"y":20,"w":360,"h":90},"text":"All systems normal. Newspaper refreshed at 05:00.","font":{"family":"sans","size":52},"wrap":true,"shrink_to_fit":true,"shrink_min_size":18,"line_height":1.2}]
	}`))
	if !result.Report.OK {
		t.Fatalf("report OK = false; issues=%+v", result.Report.Issues)
	}
	if !reportHasIssue(result.Report, "/nodes/0/font/size", "text_shrunk") {
		t.Fatalf("missing text_shrunk notice: %+v", result.Report.Issues)
	}
	if result.Report.Nodes[0].ActualFontSize == nil || *result.Report.Nodes[0].ActualFontSize >= 52 {
		t.Fatalf("text was not shrunk in report: %+v", result.Report.Nodes[0])
	}
	if result.Report.Nodes[0].Lines == nil || *result.Report.Nodes[0].Lines < 2 {
		t.Fatalf("wrapped line count missing or too small: %+v", result.Report.Nodes[0])
	}
}

func TestRenderSceneJSONReportsUnsupportedGlyph(t *testing.T) {
	result := RenderSceneJSON([]byte(`{
		"version":"lucio.scene/v1",
		"nodes":[{"id":"title","type":"text","box":{"x":0,"y":0,"w":500,"h":100},"text":"Hello 😀","font":{"size":32}}]
	}`))
	if !reportHasIssue(result.Report, "/nodes/0/text", "unsupported_glyph") {
		t.Fatalf("missing unsupported glyph issue: %+v", result.Report.Issues)
	}
}

func TestRenderSceneJSONRejectsLineStrokeOutsideBox(t *testing.T) {
	result := RenderSceneJSON([]byte(`{
		"version":"lucio.scene/v1",
		"nodes":[{"id":"rule","type":"line","box":{"x":10,"y":10,"w":20,"h":3},"from":{"x":0,"y":0},"to":{"x":19,"y":0},"stroke_width":3}]
	}`))
	if !reportHasIssue(result.Report, "/nodes/0", "line_stroke_out_of_bounds") {
		t.Fatalf("missing line stroke bounds issue: %+v", result.Report.Issues)
	}
}

func TestRenderSceneJSONDrawsImageNodeFromAssetRoot(t *testing.T) {
	dir := t.TempDir()
	asset := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	asset.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	asset.SetNRGBA(1, 0, color.NRGBA{G: 255, A: 255})
	asset.SetNRGBA(0, 1, color.NRGBA{B: 255, A: 255})
	asset.SetNRGBA(1, 1, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	writePNG(t, filepath.Join(dir, "asset.png"), asset)

	result := RenderSceneJSON([]byte(`{
		"version":"lucio.scene/v1",
		"canvas":{"background":"#000000"},
		"nodes":[{"id":"icon","type":"image","box":{"x":10,"y":20,"w":2,"h":2},"src":"asset.png"}]
	}`), RenderOptions{AssetRoot: dir})
	if !result.Report.OK {
		t.Fatalf("report OK = false; issues=%+v", result.Report.Issues)
	}
	if got := result.Image.GrayAt(10, 20).Y; got != 76 {
		t.Fatalf("red pixel gray = %d, want 76", got)
	}
	if got := result.Image.GrayAt(11, 20).Y; got != 150 {
		t.Fatalf("green pixel gray = %d, want 150", got)
	}
	if got := result.Image.GrayAt(10, 21).Y; got != 29 {
		t.Fatalf("blue pixel gray = %d, want 29", got)
	}
	if got := result.Image.GrayAt(11, 21).Y; got != 255 {
		t.Fatalf("white pixel gray = %d, want 255", got)
	}
	if got := result.Image.GrayAt(0, 0).Y; got != 0 {
		t.Fatalf("background pixel = %d, want 0", got)
	}
}

func TestRenderSceneJSONResolvesImageRelativeToScenePath(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, filepath.Join(dir, "asset.png"), image.NewGray(image.Rect(0, 0, 1, 1)))
	result := RenderSceneJSON([]byte(`{
		"version":"lucio.scene/v1",
		"nodes":[{"id":"icon","type":"image","box":{"x":0,"y":0,"w":1,"h":1},"src":"asset.png"}]
	}`), RenderOptions{ScenePath: filepath.Join(dir, "scene.json")})
	if !result.Report.OK {
		t.Fatalf("report OK = false; issues=%+v", result.Report.Issues)
	}
}

func TestRenderSceneJSONRejectsImageSizeMismatch(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, filepath.Join(dir, "asset.png"), image.NewGray(image.Rect(0, 0, 1, 1)))
	result := RenderSceneJSON([]byte(`{
		"version":"lucio.scene/v1",
		"nodes":[{"id":"icon","type":"image","box":{"x":0,"y":0,"w":2,"h":1},"src":"asset.png"}]
	}`), RenderOptions{AssetRoot: dir})
	if !reportHasIssue(result.Report, "/nodes/0/src", "asset_size_mismatch") {
		t.Fatalf("missing size mismatch issue: %+v", result.Report.Issues)
	}
	if len(result.PNG) != 0 {
		t.Fatal("PNG should not be emitted when render has errors")
	}
}

func TestRenderSceneJSONCompositesTransparentImage(t *testing.T) {
	dir := t.TempDir()
	asset := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	asset.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 128})
	writePNG(t, filepath.Join(dir, "asset.png"), asset)
	result := RenderSceneJSON([]byte(`{
		"version":"lucio.scene/v1",
		"canvas":{"background":"#000000"},
		"nodes":[{"id":"icon","type":"image","box":{"x":0,"y":0,"w":1,"h":1},"src":"asset.png"}]
	}`), RenderOptions{AssetRoot: dir})
	if !result.Report.OK {
		t.Fatalf("report OK = false; issues=%+v", result.Report.Issues)
	}
	if got := result.Image.GrayAt(0, 0).Y; got != 38 {
		t.Fatalf("composited transparent red over black = %d, want 38", got)
	}
}

func countNonWhite(img *image.Gray, rect image.Rectangle) int {
	n := 0
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			if img.GrayAt(x, y).Y != 255 {
				n++
			}
		}
	}
	return n
}

func writePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func TestRenderSceneJSONRightAlignedTextWithOverhangingInkFits(t *testing.T) {
	// Regression: right-aligned text whose ink extends slightly past its
	// final advance (e.g. "Battery 87%" in Noto Sans Mono) used to be
	// placed so the ink's right edge exceeded the box by a subpixel, which
	// ceil turned into a text_overflow error.
	result := RenderSceneJSON([]byte(`{
		"version":"lucio.scene/v1",
		"nodes":[
			{"id":"legend","type":"text","box":{"x":864,"y":128,"w":512,"h":48},"text":"Battery 87%","font":{"family":"mono","size":24},"align":"right","valign":"top"}
		]
	}`))
	if !result.Report.OK {
		t.Fatalf("report OK = false; issues=%+v", result.Report.Issues)
	}
}
