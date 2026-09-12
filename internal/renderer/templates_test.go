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
	const wantSHA256 = "a7c6e2cbebae8deda8a70d48bdd45acff09c530edc27a81727e68aa21d6ab60c"
	if result.Report.Output.SHA256 != wantSHA256 {
		t.Fatalf("dashboard sha256 = %s, want %s", result.Report.Output.SHA256, wantSHA256)
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
