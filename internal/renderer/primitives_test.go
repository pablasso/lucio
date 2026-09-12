package renderer

import (
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRulePrimitiveRendersLineAndReportsDrawOp(t *testing.T) {
	result := RenderSceneJSON([]byte(`{
		"version":"lucio.scene/v1",
		"nodes":[{"id":"divider","type":"rule","box":{"x":10,"y":10,"w":20,"h":5},"stroke_width":3,"inset_start":2,"inset_end":3}]
	}`))
	if !result.Report.OK {
		t.Fatalf("report OK = false; issues=%+v", result.Report.Issues)
	}
	if len(result.Report.DrawOps) != 1 || result.Report.DrawOps[0].ID != "divider.__line" || result.Report.DrawOps[0].Role != "rule.line" {
		t.Fatalf("unexpected draw ops: %+v", result.Report.DrawOps)
	}
	for _, y := range []int{11, 12, 13} {
		if got := result.Image.GrayAt(12, y).Y; got != 0 {
			t.Fatalf("rule pixel y=%d = %d, want 0", y, got)
		}
	}
	if got := result.Image.GrayAt(10, 12).Y; got != 255 {
		t.Fatalf("inset pixel = %d, want background", got)
	}
}

func TestGridAssignsCardBoxesAndRendersChildren(t *testing.T) {
	result := RenderSceneJSON([]byte(`{
		"version":"lucio.scene/v1",
		"nodes":[{
			"id":"cards","type":"grid","box":{"x":10,"y":20,"w":100,"h":40},"columns":2,"gap_x":1,"gap_y":0,
			"children":[
				{"id":"left","type":"card","fill":"#000000","stroke_width":0},
				{"id":"right","type":"card","fill":"#333333","stroke_width":0}
			]
		}]
	}`))
	if !result.Report.OK {
		t.Fatalf("report OK = false; issues=%+v", result.Report.Issues)
	}
	if len(result.Report.Nodes) != 3 {
		t.Fatalf("nodes = %d, want grid + 2 cards: %+v", len(result.Report.Nodes), result.Report.Nodes)
	}
	grid := result.Report.Nodes[0]
	if grid.Type != "grid" || grid.Layout == nil || grid.Layout.Grid == nil {
		t.Fatalf("missing grid layout: %+v", grid)
	}
	if got := grid.Layout.Grid.CellWidths; len(got) != 2 || got[0] != 50 || got[1] != 49 {
		t.Fatalf("cell widths = %+v, want [50 49]", got)
	}
	left := result.Report.Nodes[1]
	if left.ID != "left" || left.ParentID != "cards" || left.BoxSource != BoxSourceAssigned || left.Box != (Box{X: 10, Y: 20, W: 50, H: 40}) {
		t.Fatalf("unexpected left card report: %+v", left)
	}
	right := result.Report.Nodes[2]
	if right.ID != "right" || right.Box != (Box{X: 61, Y: 20, W: 49, H: 40}) {
		t.Fatalf("unexpected right card report: %+v", right)
	}
	if got := result.Image.GrayAt(20, 30).Y; got != 0 {
		t.Fatalf("left card pixel = %d, want black", got)
	}
	if got := result.Image.GrayAt(70, 30).Y; got != 51 {
		t.Fatalf("right card pixel = %d, want #333 gray", got)
	}
}

func TestGridRejectsChildBoxesAndDuplicateNestedIDs(t *testing.T) {
	_, report := ValidateSceneJSON([]byte(`{
		"version":"lucio.scene/v1",
		"nodes":[{
			"id":"grid","type":"grid","box":{"x":0,"y":0,"w":100,"h":100},"columns":1,
			"children":[
				{"id":"grid","type":"card","box":{"x":0,"y":0,"w":10,"h":10}}
			]
		}]
	}`))
	if !reportHasIssue(report, "/nodes/0/children/0/box", "box_not_allowed") {
		t.Fatalf("missing child box issue: %+v", report.Issues)
	}
	if !reportHasIssue(report, "/nodes/0/children/0/id", "duplicate_id") {
		t.Fatalf("missing nested duplicate id issue: %+v", report.Issues)
	}
}

func TestGeneratedDrawOpCollisionIsReported(t *testing.T) {
	result := RenderSceneJSON([]byte(`{
		"version":"lucio.scene/v1",
		"nodes":[
			{"id":"card","type":"card","box":{"x":0,"y":0,"w":100,"h":100}},
			{"id":"card.__bg","type":"rect","box":{"x":120,"y":0,"w":10,"h":10},"fill":"#000000"}
		]
	}`))
	if result.Report.OK {
		t.Fatal("report OK = true, want generated draw op collision")
	}
	if !reportHasIssue(result.Report, "/nodes/0", "draw_op_id_collision") {
		t.Fatalf("missing generated collision issue: %+v", result.Report.Issues)
	}
}

func TestCardBodyOverflowMapsToAuthorPathAndDrawOp(t *testing.T) {
	result := RenderSceneJSON([]byte(`{
		"version":"lucio.scene/v1",
		"nodes":[{"id":"fact","type":"card","box":{"x":10,"y":10,"w":200,"h":90},"body":{"text":"This body text is intentionally much too tall for the small card body area.","font":{"size":42},"wrap":true}}]
	}`))
	if result.Report.OK {
		t.Fatal("report OK = true, want text overflow")
	}
	found := false
	for _, issue := range result.Report.Issues {
		if issue.Code == "text_overflow" && issue.Node == "fact" && issue.DrawOp == "fact.__body_text" && issue.Path == "/nodes/0/body/text" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing source-mapped card body overflow: %+v", result.Report.Issues)
	}
}

func TestListPrimitiveRendersMarkersAndItems(t *testing.T) {
	result := RenderSceneJSON([]byte(`{
		"version":"lucio.scene/v1",
		"nodes":[{"id":"todos","type":"list","box":{"x":10,"y":10,"w":240,"h":120},"items":["First item wraps nicely","Second"],"marker":"dash","font":{"size":20},"item_gap":4}]
	}`))
	if !result.Report.OK {
		t.Fatalf("report OK = false; issues=%+v", result.Report.Issues)
	}
	if len(result.Report.Nodes) != 1 || result.Report.Nodes[0].Layout == nil || result.Report.Nodes[0].Layout.List == nil {
		t.Fatalf("missing list layout: %+v", result.Report.Nodes)
	}
	layout := result.Report.Nodes[0].Layout.List
	if layout.Marker != "dash" || layout.ItemCount != 2 || len(layout.Items) != 2 || layout.MarkerColumnWidth <= 0 {
		t.Fatalf("unexpected list layout: %+v", layout)
	}
	if len(result.Report.DrawOps) != 4 {
		t.Fatalf("draw ops = %d, want 4 marker/item ops: %+v", len(result.Report.DrawOps), result.Report.DrawOps)
	}
	if changed := countNonWhite(result.Image, image.Rect(10, 10, 250, 130)); changed == 0 {
		t.Fatal("list drew no pixels")
	}
}

func TestListCheckMarkerDrawsGeometrically(t *testing.T) {
	result := RenderSceneJSON([]byte(`{
		"version":"lucio.scene/v1",
		"nodes":[{"id":"checks","type":"list","box":{"x":10,"y":10,"w":240,"h":80},"items":["Done"],"marker":"check","font":{"size":20}}]
	}`))
	if !result.Report.OK {
		t.Fatalf("report OK = false; issues=%+v", result.Report.Issues)
	}
	if got := result.Report.Nodes[0].DrawOps; len(got) != 3 || got[0] != "checks.__marker_0_a" || got[1] != "checks.__marker_0_b" || got[2] != "checks.__item_0" {
		t.Fatalf("unexpected check marker draw ops: %+v", got)
	}
	if changed := countNonWhite(result.Image, image.Rect(10, 10, 250, 90)); changed == 0 {
		t.Fatal("check list drew no pixels")
	}
}

func TestListRejectsUnbreakableItemWidth(t *testing.T) {
	result := RenderSceneJSON([]byte(`{
		"version":"lucio.scene/v1",
		"nodes":[{"id":"bad-list","type":"list","box":{"x":0,"y":0,"w":80,"h":120},"items":["Supercalifragilisticexpialidocious"],"font":{"size":24}}]
	}`))
	if result.Report.OK {
		t.Fatal("report OK = true, want list overflow")
	}
	if !reportHasIssue(result.Report, "/nodes/0/items", "list_overflow") {
		t.Fatalf("missing list overflow issue: %+v", result.Report.Issues)
	}
}

func TestListShrinkToFitReducesLayoutFontSize(t *testing.T) {
	result := RenderSceneJSON([]byte(`{
		"version":"lucio.scene/v1",
		"nodes":[{"id":"small","type":"list","box":{"x":0,"y":0,"w":260,"h":50},"items":["A long item that wraps to multiple lines","Another long item"],"font":{"size":24},"item_gap":2,"shrink_to_fit":true,"shrink_min_size":10}]
	}`))
	if !result.Report.OK {
		t.Fatalf("report OK = false; issues=%+v", result.Report.Issues)
	}
	layout := result.Report.Nodes[0].Layout.List
	if layout.FontSize >= 24 {
		t.Fatalf("list did not shrink: %+v", layout)
	}
}

func TestStackAssignsChildBoxes(t *testing.T) {
	result := RenderSceneJSON([]byte(`{
		"version":"lucio.scene/v1",
		"nodes":[{"id":"stack","type":"stack","box":{"x":10,"y":20,"w":100,"h":80},"gap":5,"children":[
			{"height":30,"node":{"id":"top","type":"card","fill":"#000000","stroke_width":0,"radius":0}},
			{"height":20,"node":{"id":"bottom","type":"card","fill":"#333333","stroke_width":0,"radius":0}}
		]}]
	}`))
	if !result.Report.OK {
		t.Fatalf("report OK = false; issues=%+v", result.Report.Issues)
	}
	if len(result.Report.Nodes) != 3 {
		t.Fatalf("nodes = %d, want stack + 2 children: %+v", len(result.Report.Nodes), result.Report.Nodes)
	}
	if result.Report.Nodes[0].Layout == nil || result.Report.Nodes[0].Layout.Stack == nil {
		t.Fatalf("missing stack layout: %+v", result.Report.Nodes[0])
	}
	if result.Report.Nodes[1].ID != "top" || result.Report.Nodes[1].Box != (Box{X: 10, Y: 20, W: 100, H: 30}) || result.Report.Nodes[1].BoxSource != BoxSourceAssigned {
		t.Fatalf("unexpected top child: %+v", result.Report.Nodes[1])
	}
	if result.Report.Nodes[2].ID != "bottom" || result.Report.Nodes[2].Box != (Box{X: 10, Y: 55, W: 100, H: 20}) {
		t.Fatalf("unexpected bottom child: %+v", result.Report.Nodes[2])
	}
	if got := result.Image.GrayAt(20, 25).Y; got != 0 {
		t.Fatalf("top card pixel = %d, want black", got)
	}
	if got := result.Image.GrayAt(20, 60).Y; got != 51 {
		t.Fatalf("bottom card pixel = %d, want #333 gray", got)
	}
}

func TestSectionRendersTitleRuleAndAssignsChildren(t *testing.T) {
	result := RenderSceneJSON([]byte(`{
		"version":"lucio.scene/v1",
		"nodes":[{"id":"sec","type":"section","box":{"x":10,"y":20,"w":300,"h":160},"title":"Today","title_font":{"size":24,"weight":"bold"},"title_height":40,"gap":10,"rule_height":4,"rule_stroke_width":2,"children":[
			{"height":40,"node":{"id":"child","type":"card","fill":"#333333","stroke_width":0}}
		]}]
	}`))
	if !result.Report.OK {
		t.Fatalf("report OK = false; issues=%+v", result.Report.Issues)
	}
	if len(result.Report.Nodes) != 2 || result.Report.Nodes[0].Layout == nil || result.Report.Nodes[0].Layout.Section == nil {
		t.Fatalf("unexpected section nodes: %+v", result.Report.Nodes)
	}
	section := result.Report.Nodes[0].Layout.Section
	if section.TitleBox == nil || *section.TitleBox != (Box{X: 10, Y: 20, W: 300, H: 40}) {
		t.Fatalf("unexpected title box: %+v", section.TitleBox)
	}
	if section.RuleBox == nil || *section.RuleBox != (Box{X: 10, Y: 70, W: 300, H: 4}) {
		t.Fatalf("unexpected rule box: %+v", section.RuleBox)
	}
	if section.ContentBox == nil || *section.ContentBox != (Box{X: 10, Y: 84, W: 300, H: 96}) {
		t.Fatalf("unexpected content box: %+v", section.ContentBox)
	}
	if result.Report.Nodes[1].ID != "child" || result.Report.Nodes[1].Box != (Box{X: 10, Y: 84, W: 300, H: 40}) {
		t.Fatalf("unexpected section child: %+v", result.Report.Nodes[1])
	}
	if len(result.Report.Nodes[0].DrawOps) != 2 || result.Report.Nodes[0].DrawOps[0] != "sec.__title_text" || result.Report.Nodes[0].DrawOps[1] != "sec.__rule_line" {
		t.Fatalf("unexpected section draw ops: %+v", result.Report.Nodes[0].DrawOps)
	}
	if got := result.Image.GrayAt(20, 90).Y; got != 51 {
		t.Fatalf("child card pixel = %d, want #333 gray", got)
	}
}

func TestSectionOverflowMentionsAvailableChildSpace(t *testing.T) {
	result := RenderSceneJSON([]byte(`{
		"version":"lucio.scene/v1",
		"nodes":[{"id":"sec","type":"section","box":{"x":0,"y":0,"w":300,"h":100},"title":"Too tight","title_height":50,"gap":10,"rule_height":4,"children":[
			{"height":40,"node":{"id":"child","type":"card","fill":"#333333","stroke_width":0,"radius":0}}
		]}]
	}`))
	if result.Report.OK {
		t.Fatal("report OK = true, want section overflow")
	}
	var found bool
	for _, issue := range result.Report.Issues {
		if issue.Path == "/nodes/0/children" && issue.Code == "section_overflow" && strings.Contains(issue.Message, "only 26px is available") && strings.Contains(issue.Message, "title_height=50") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing helpful section overflow issue: %+v", result.Report.Issues)
	}
}

func TestImageNodeSupportsJPEG(t *testing.T) {
	dir := t.TempDir()
	assetPath := filepath.Join(dir, "white.jpg")
	f, err := os.Create(assetPath)
	if err != nil {
		t.Fatal(err)
	}
	asset := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	asset.SetNRGBA(0, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	if err := jpeg.Encode(f, asset, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	result := RenderSceneJSON([]byte(`{
		"version":"lucio.scene/v1",
		"canvas":{"background":"#000000"},
		"nodes":[{"id":"photo","type":"image","box":{"x":0,"y":0,"w":1,"h":1},"src":"white.jpg"}]
	}`), RenderOptions{AssetRoot: dir})
	if !result.Report.OK {
		t.Fatalf("report OK = false; issues=%+v", result.Report.Issues)
	}
	if got := result.Image.GrayAt(0, 0).Y; got != 255 {
		t.Fatalf("jpeg pixel = %d, want white", got)
	}
}

func TestImageFitContainReportsScaleAndBackground(t *testing.T) {
	dir := t.TempDir()
	asset := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	asset.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	asset.SetNRGBA(1, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	writeTestPNG(t, filepath.Join(dir, "wide.png"), asset)

	result := RenderSceneJSON([]byte(`{
		"version":"lucio.scene/v1",
		"nodes":[{"id":"hero","type":"image","box":{"x":0,"y":0,"w":4,"h":4},"src":"wide.png","fit":"contain","background":"#000000"}]
	}`), RenderOptions{AssetRoot: dir})
	if !result.Report.OK {
		t.Fatalf("report OK = false; issues=%+v", result.Report.Issues)
	}
	if got := result.Image.GrayAt(0, 0).Y; got != 0 {
		t.Fatalf("letterbox background = %d, want black", got)
	}
	if len(result.Report.DrawOps) != 1 || result.Report.DrawOps[0].Image == nil {
		t.Fatalf("missing image fit draw op report: %+v", result.Report.DrawOps)
	}
	imageReport := result.Report.DrawOps[0].Image
	if imageReport.Fit != "contain" || imageReport.ScaledSize == nil || imageReport.ScaledSize.W != 4 || imageReport.ScaledSize.H != 2 {
		t.Fatalf("unexpected image fit report: %+v", imageReport)
	}
	if imageReport.FinalDrawRect == nil || *imageReport.FinalDrawRect != (Box{X: 0, Y: 1, W: 4, H: 2}) {
		t.Fatalf("unexpected final draw rect: %+v", imageReport.FinalDrawRect)
	}
}

func writeTestPNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
