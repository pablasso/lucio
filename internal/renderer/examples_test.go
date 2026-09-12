package renderer

import (
	"os"
	"testing"
)

func TestShippedExamples(t *testing.T) {
	for _, name := range []string{"hello", "dashboard", "image"} {
		t.Run(name, func(t *testing.T) {
			path := "../../examples/" + name + ".json"
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			_, report := ValidateSceneJSON(data)
			if !report.OK {
				t.Fatal(report.Issues)
			}
			if name != "image" {
				result := RenderSceneJSON(data, RenderOptions{ScenePath: path})
				if !result.Report.OK {
					t.Fatal(result.Report.Issues)
				}
			}
		})
	}
}
