package renderer

import (
	"encoding/json"
	"testing"
)

func TestSchemaJSONIsVersionedJSONSchema(t *testing.T) {
	data, err := SchemaJSON(SceneVersionV1)
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	if schema["$id"] != SchemaURLV1 {
		t.Fatalf("schema $id = %v, want %s", schema["$id"], SchemaURLV1)
	}
	if schema["title"] == "" || schema["$defs"] == nil {
		t.Fatalf("schema missing title or defs: %+v", schema)
	}
}

func TestTemplateJSONProducesValidScenes(t *testing.T) {
	names := TemplateNames()
	if len(names) != 4 {
		t.Fatalf("TemplateNames len = %d, want 4", len(names))
	}
	for _, name := range names {
		data, err := TemplateJSON(name)
		if err != nil {
			t.Fatalf("TemplateJSON(%q): %v", name, err)
		}
		_, report := ValidateSceneJSON(data)
		if !report.OK {
			t.Fatalf("template %q is invalid: %+v", name, report.Issues)
		}
	}
}

func TestDashboardTemplateGoldenHash(t *testing.T) {
	data, err := TemplateJSON(TemplateDashboard)
	if err != nil {
		t.Fatal(err)
	}
	result := RenderSceneJSON(data)
	if !result.Report.OK {
		t.Fatalf("dashboard failed to render: %+v", result.Report.Issues)
	}
	// x/image/vector's font rasterization differs with fused multiply-add
	// (FMA): arm64 uses it, baseline amd64 does not. Disabling FMA in vector
	// on arm64 reproduces the amd64 PNG exactly. The reviewed outputs differ
	// at 52 antialiased pixels by just 1/255; all 16-level display pixels and
	// layout metrics are identical. Keep exact baselines, not a platform skip
	// or a broad pixel tolerance. New hashes still require visual review.
	const (
		wantFMA   = "a7c6e2cbebae8deda8a70d48bdd45acff09c530edc27a81727e68aa21d6ab60c"
		wantNoFMA = "312fdd61bfedbbc780eef9877c8e3af956ed498bb5fbf4183137519d67616f24"
	)
	switch got := result.Report.Output.SHA256; got {
	case wantFMA, wantNoFMA:
	default:
		t.Fatalf("dashboard sha256 = %s, want reviewed FMA %s or non-FMA %s", got, wantFMA, wantNoFMA)
	}
}

func TestRenderableTemplatesRender(t *testing.T) {
	for _, name := range []string{TemplateDashboard, TemplateStatusCard, TemplateTwoColumn} {
		data, err := TemplateJSON(name)
		if err != nil {
			t.Fatal(err)
		}
		result := RenderSceneJSON(data)
		if !result.Report.OK {
			t.Fatalf("template %q failed to render: %+v", name, result.Report.Issues)
		}
		if len(result.PNG) == 0 || result.Report.Output.SHA256 == "" {
			t.Fatalf("template %q did not produce PNG metadata", name)
		}
	}
}
