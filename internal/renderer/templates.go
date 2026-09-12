package renderer

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	TemplateDashboard       = "dashboard"
	TemplateImageFullscreen = "image-fullscreen"
	TemplateStatusCard      = "status-card"
	TemplateTwoColumn       = "two-column"
)

func TemplateNames() []string {
	names := []string{TemplateDashboard, TemplateImageFullscreen, TemplateStatusCard, TemplateTwoColumn}
	sort.Strings(names)
	return names
}

func TemplateJSON(name string) ([]byte, error) {
	scene, err := TemplateScene(name)
	if err != nil {
		return nil, err
	}
	b, err := json.MarshalIndent(scene, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func TemplateScene(name string) (Scene, error) {
	scene := baseTemplateScene()
	switch name {
	case TemplateDashboard:
		scene.Nodes = []Node{
			{
				ID:     "title",
				Type:   NodeTypeText,
				Box:    Box{X: 80, Y: 80, W: 1280, H: 120},
				Text:   "Kitchen dashboard",
				Font:   &Font{Family: FontFamilySans, Size: 64, Weight: FontWeightBold},
				Align:  AlignCenter,
				VAlign: VAlignMiddle,
				Color:  DefaultTextColor,
			},
			{
				ID:          "card",
				Type:        NodeTypeRect,
				Box:         Box{X: 80, Y: 240, W: 1280, H: 360},
				Fill:        "#eeeeee",
				Stroke:      DefaultTextColor,
				StrokeWidth: intPtr(3),
				Radius:      intPtr(24),
			},
			{
				ID:            "body",
				Type:          NodeTypeText,
				Box:           Box{X: 120, Y: 290, W: 1200, H: 260},
				Text:          "All systems normal. Newspaper refreshed at 05:00.",
				Font:          &Font{Family: FontFamilySans, Size: 48},
				Wrap:          boolPtr(true),
				ShrinkToFit:   boolPtr(true),
				ShrinkMinSize: intPtr(DefaultShrinkMinSize),
				Color:         "#111111",
			},
		}
	case TemplateImageFullscreen:
		scene.Nodes = []Node{
			{
				ID:   "image",
				Type: NodeTypeImage,
				Box:  Box{X: 0, Y: 0, W: CanvasWidth, H: CanvasHeight},
				Src:  "image-1440x2560.png",
			},
		}
	case TemplateStatusCard:
		scene.Nodes = []Node{
			{
				ID:          "card",
				Type:        NodeTypeRect,
				Box:         Box{X: 100, Y: 160, W: 1240, H: 520},
				Fill:        "#f3f3f3",
				Stroke:      DefaultTextColor,
				StrokeWidth: intPtr(3),
				Radius:      intPtr(20),
			},
			{
				ID:    "status",
				Type:  NodeTypeText,
				Box:   Box{X: 150, Y: 220, W: 1140, H: 100},
				Text:  "System status",
				Font:  &Font{Family: FontFamilySans, Size: 54, Weight: FontWeightBold},
				Color: DefaultTextColor,
			},
			{
				ID:          "rule",
				Type:        NodeTypeLine,
				Box:         Box{X: 150, Y: 350, W: 1140, H: 5},
				From:        &Point{X: 0, Y: 2},
				To:          &Point{X: 1139, Y: 2},
				Stroke:      "#888888",
				StrokeWidth: intPtr(3),
			},
			{
				ID:            "message",
				Type:          NodeTypeText,
				Box:           Box{X: 150, Y: 390, W: 1140, H: 220},
				Text:          "Everything is healthy. Last update: 05:00.",
				Font:          &Font{Family: FontFamilySans, Size: 42},
				Wrap:          boolPtr(true),
				ShrinkToFit:   boolPtr(true),
				ShrinkMinSize: intPtr(18),
				Color:         "#222222",
			},
		}
	case TemplateTwoColumn:
		scene.Nodes = []Node{
			{
				ID:     "title",
				Type:   NodeTypeText,
				Box:    Box{X: 80, Y: 70, W: 1280, H: 110},
				Text:   "Daily overview",
				Font:   &Font{Family: FontFamilySerif, Size: 60, Weight: FontWeightBold},
				Align:  AlignCenter,
				VAlign: VAlignMiddle,
				Color:  DefaultTextColor,
			},
			{
				ID:          "left_card",
				Type:        NodeTypeRect,
				Box:         Box{X: 80, Y: 240, W: 610, H: 900},
				Fill:        "#f2f2f2",
				Stroke:      "#000000",
				StrokeWidth: intPtr(2),
				Radius:      intPtr(18),
			},
			{
				ID:          "right_card",
				Type:        NodeTypeRect,
				Box:         Box{X: 750, Y: 240, W: 610, H: 900},
				Fill:        "#f2f2f2",
				Stroke:      "#000000",
				StrokeWidth: intPtr(2),
				Radius:      intPtr(18),
			},
			{
				ID:            "left_text",
				Type:          NodeTypeText,
				Box:           Box{X: 120, Y: 300, W: 530, H: 780},
				Text:          "Left column\n\nUse this area for weather, calendar, or reminders.",
				Font:          &Font{Family: FontFamilySans, Size: 42},
				Wrap:          boolPtr(true),
				ShrinkToFit:   boolPtr(true),
				ShrinkMinSize: intPtr(18),
				Color:         "#111111",
			},
			{
				ID:            "right_text",
				Type:          NodeTypeText,
				Box:           Box{X: 790, Y: 300, W: 530, H: 780},
				Text:          "Right column\n\nUse this area for status, notes, or a short checklist.",
				Font:          &Font{Family: FontFamilyMono, Size: 36},
				Wrap:          boolPtr(true),
				ShrinkToFit:   boolPtr(true),
				ShrinkMinSize: intPtr(16),
				Color:         "#111111",
			},
		}
	default:
		return Scene{}, fmt.Errorf("unknown template %q (available: %s)", name, strings.Join(TemplateNames(), ", "))
	}
	return scene, nil
}

func baseTemplateScene() Scene {
	return Scene{
		Schema:  SchemaURLV1,
		Version: SceneVersionV1,
		Canvas:  Canvas{Width: CanvasWidth, Height: CanvasHeight, Background: DefaultCanvasBackground},
		Nodes:   []Node{},
	}
}

func boolPtr(v bool) *bool { return &v }
