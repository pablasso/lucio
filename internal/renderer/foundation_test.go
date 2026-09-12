package renderer

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCompileSceneJSONEmitsLowLevelDisplayList(t *testing.T) {
	compiled, report := CompileSceneJSON([]byte(`{
		"version":"lucio.scene/v1",
		"nodes":[
			{"id":"panel","type":"rect","box":{"x":10,"y":20,"w":30,"h":40},"fill":"#eeeeee"},
			{"id":"title","type":"text","box":{"x":50,"y":60,"w":300,"h":80},"text":"Hello","font":{"size":24}}
		]
	}`))
	if !report.OK {
		t.Fatalf("compile report OK = false; issues=%+v", report.Issues)
	}
	if len(compiled.Author.Nodes) != 2 || compiled.Author.Nodes[0].Path != "/nodes/0" || compiled.Author.Nodes[1].Path != "/nodes/1" {
		t.Fatalf("unexpected author nodes: %+v", compiled.Author.Nodes)
	}
	if len(compiled.Resolved.Nodes) != 2 || compiled.Resolved.Nodes[0].BoxSource != BoxSourceExplicit {
		t.Fatalf("unexpected resolved nodes: %+v", compiled.Resolved.Nodes)
	}
	if len(compiled.DisplayList.Ops) != 2 {
		t.Fatalf("display ops = %d, want 2", len(compiled.DisplayList.Ops))
	}
	if op := compiled.DisplayList.Ops[0]; op.ID != "panel" || op.Command.Rect == nil || op.Source.Path != "/nodes/0" || op.Source.FieldPath["fill"] != "/nodes/0/fill" {
		t.Fatalf("unexpected rect op: %+v", op)
	}
	if op := compiled.DisplayList.Ops[1]; op.ID != "title" || op.Command.Text == nil || op.Source.Path != "/nodes/1" || op.Source.FieldPath["text"] != "/nodes/1/text" {
		t.Fatalf("unexpected text op: %+v", op)
	}
	if len(report.DrawOps) != 0 {
		t.Fatalf("default compile report should omit draw ops: %+v", report.DrawOps)
	}
}

func TestRenderSceneJSONDefaultReportOmitsFoundationDetails(t *testing.T) {
	result := RenderSceneJSON([]byte(`{
		"version":"lucio.scene/v1",
		"nodes":[{"id":"panel","type":"rect","box":{"x":0,"y":0,"w":10,"h":10},"fill":"#ffffff"}]
	}`))
	if !result.Report.OK {
		t.Fatalf("report OK = false; issues=%+v", result.Report.Issues)
	}
	data, err := json.Marshal(result.Report)
	if err != nil {
		t.Fatal(err)
	}
	jsonText := string(data)
	for _, forbidden := range []string{"draw_ops", "box_source", "source_path"} {
		if strings.Contains(jsonText, forbidden) {
			t.Fatalf("default low-level report contains %q: %s", forbidden, jsonText)
		}
	}
}

func TestRenderSceneJSONReportDetailIncludesDrawOps(t *testing.T) {
	result := RenderSceneJSON([]byte(`{
		"version":"lucio.scene/v1",
		"nodes":[{"id":"title","type":"text","box":{"x":20,"y":20,"w":300,"h":80},"text":"Hello","font":{"size":24}}]
	}`), RenderOptions{ReportDetail: true})
	if !result.Report.OK {
		t.Fatalf("report OK = false; issues=%+v", result.Report.Issues)
	}
	if len(result.Report.Nodes) != 1 || result.Report.Nodes[0].Path != "/nodes/0" || result.Report.Nodes[0].BoxSource != BoxSourceExplicit {
		t.Fatalf("detail node report missing resolved fields: %+v", result.Report.Nodes)
	}
	if len(result.Report.Nodes[0].DrawOps) != 1 || result.Report.Nodes[0].DrawOps[0] != "title" {
		t.Fatalf("detail node report missing draw op ids: %+v", result.Report.Nodes[0])
	}
	if len(result.Report.DrawOps) != 1 {
		t.Fatalf("draw op reports = %d, want 1", len(result.Report.DrawOps))
	}
	draw := result.Report.DrawOps[0]
	if draw.ID != "title" || draw.Type != "text" || draw.SourceID != "title" || draw.SourcePath != "/nodes/0" || draw.ActualFontSize == nil || *draw.ActualFontSize != 24 {
		t.Fatalf("unexpected draw op report: %+v", draw)
	}
}

func TestSourceMapGeneratedDrawOpIssuePath(t *testing.T) {
	op := DrawOp{
		ID:   "card.__body_text",
		Role: "card.body_text",
		Source: SourceRef{
			ID:   "card",
			Type: "card",
			Path: "/nodes/2",
			FieldPath: map[string]string{
				"text": "/nodes/2/body/text",
				"font": "/nodes/2/body/font",
			},
		},
		Command:   CoreCommand{Text: &CoreTextCommand{Box: Box{X: 0, Y: 0, W: 10, H: 10}}},
		Generated: true,
	}
	mapped := sourceMapIssues([]Issue{errorIssue("card.__body_text", "/nodes/7/text", "text_overflow", "too tall")}, op, 7)
	if len(mapped) != 1 {
		t.Fatalf("mapped issues = %d, want 1", len(mapped))
	}
	if mapped[0].Node != "card" || mapped[0].DrawOp != "card.__body_text" || mapped[0].Path != "/nodes/2/body/text" {
		t.Fatalf("unexpected mapped issue: %+v", mapped[0])
	}

	mapped = sourceMapIssues([]Issue{errorIssue("card.__body_text", "/nodes/7/font/size", "out_of_range", "bad size")}, op, 7)
	if mapped[0].Path != "/nodes/2/body/font/size" {
		t.Fatalf("font subpath was not mapped through source field path: %+v", mapped[0])
	}
}

func TestValidateDrawOpIDsRejectsGeneratedAuthorCollision(t *testing.T) {
	issues := validateDrawOpIDs(DisplayList{Ops: []DrawOp{
		{ID: "card.__bg", Source: SourceRef{ID: "card", Path: "/nodes/0"}, Generated: true},
		{ID: "existing", Source: SourceRef{ID: "card", Path: "/nodes/0"}, Generated: true},
	}}, map[string]bool{"card": true, "existing": true})
	if len(issues) != 1 || issues[0].Code != "draw_op_id_collision" || issues[0].Node != "card" {
		t.Fatalf("unexpected collision issues: %+v", issues)
	}
}
