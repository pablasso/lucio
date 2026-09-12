package renderer

import "testing"

func TestValidateSceneJSONAcceptsMinimalValidScene(t *testing.T) {
	_, report := ValidateSceneJSON([]byte(`{
		"version":"lucio.scene/v1",
		"nodes":[
			{"id":"card","type":"rect","box":{"x":80,"y":240,"w":1280,"h":360},"fill":"#eeeeee","stroke":"#000000","stroke_width":3,"radius":24},
			{"id":"rule","type":"line","box":{"x":80,"y":640,"w":1280,"h":8},"from":{"x":0,"y":4},"to":{"x":1279,"y":4}},
			{"id":"newspaper","type":"image","box":{"x":0,"y":0,"w":1440,"h":2560},"src":"today-newspaper-1440x2560.png"}
		]
	}`))
	if !report.OK {
		t.Fatalf("report OK = false; issues=%+v", report.Issues)
	}
	if report.SceneVersion != SceneVersionV1 {
		t.Fatalf("scene_version = %q", report.SceneVersion)
	}
	if report.Issues == nil {
		t.Fatal("report issues is nil, want empty slice")
	}
	if len(report.Nodes) != 3 {
		t.Fatalf("report nodes = %d, want 3", len(report.Nodes))
	}
}

func TestValidateSceneJSONReportsStrictErrors(t *testing.T) {
	_, report := ValidateSceneJSON([]byte(`{
		"version":"lucio.scene/v1",
		"extra":true,
		"canvas":{"width":1400,"height":2560,"background":"white"},
		"nodes":[
			{"id":"dup","type":"text","box":{"x":0,"y":0,"w":100,"h":100},"text":"hi","font":{"size":0,"family":"comic"},"src":"wrong.png"},
			{"id":"dup","type":"image","box":{"x":1400,"y":2500,"w":100,"h":100},"src":"https://example.com/a.jpg"},
			{"id":"bad","type":"unknown","box":{"x":0,"y":0,"w":10,"h":10}}
		]
	}`))
	if report.OK {
		t.Fatal("report OK = true, want false")
	}
	want := map[string]string{
		"/extra":               "unknown_field",
		"/canvas/width":        "bad_canvas",
		"/canvas/background":   "bad_color",
		"/nodes/0/src":         "unknown_field",
		"/nodes/0/font/size":   "out_of_range",
		"/nodes/0/font/family": "bad_enum",
		"/nodes/1/box":         "box_out_of_bounds",
		"/nodes/1/src":         "bad_asset",
		"/nodes/2/type":        "bad_enum",
		"/nodes/1/id":          "duplicate_id",
	}
	for path, code := range want {
		if !reportHasIssue(report, path, code) {
			t.Fatalf("missing issue %s %s; got %+v", path, code, report.Issues)
		}
	}
}

func TestValidateSceneJSONRejectsNodesNull(t *testing.T) {
	_, report := ValidateSceneJSON([]byte(`{"version":"lucio.scene/v1","nodes":null}`))
	if !reportHasIssue(report, "/nodes", "invalid_type") {
		t.Fatalf("expected /nodes invalid_type; got %+v", report.Issues)
	}
}

func TestValidateSceneJSONReportsJSONPointerEscapedUnknownField(t *testing.T) {
	_, report := ValidateSceneJSON([]byte(`{"version":"lucio.scene/v1","nodes":[],"a/b~c":1}`))
	if !reportHasIssue(report, "/a~1b~0c", "unknown_field") {
		t.Fatalf("expected escaped unknown field path; got %+v", report.Issues)
	}
}

func reportHasIssue(report Report, path, code string) bool {
	for _, issue := range report.Issues {
		if issue.Path == path && issue.Code == code {
			return true
		}
	}
	return false
}
