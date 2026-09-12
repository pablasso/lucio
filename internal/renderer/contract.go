package renderer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

const (
	DefaultCanvasBackground = "#ffffff"
	DefaultTextColor        = "#000000"
	DefaultLineHeight       = 1.2
	DefaultShrinkMinSize    = 12
)

// Font aliases are part of the public v1 contract. The text renderer must use
// bundled font files only, with no system font discovery.
type FontAlias struct {
	Family      FontFamily `json:"family"`
	RegularFace string     `json:"regular_face"`
	BoldFace    string     `json:"bold_face"`
}

func FontAliases() []FontAlias {
	return []FontAlias{
		{Family: FontFamilySans, RegularFace: "Noto Sans Regular", BoldFace: "Noto Sans Bold"},
		{Family: FontFamilySerif, RegularFace: "Noto Serif Regular", BoldFace: "Noto Serif Bold"},
		{Family: FontFamilyMono, RegularFace: "Noto Sans Mono Regular", BoldFace: "Noto Sans Mono Bold"},
	}
}

type PrimitiveMetadata struct {
	Type     NodeType       `json:"type"`
	Required []string       `json:"required"`
	Fields   map[string]any `json:"fields"`
	Notes    []string       `json:"notes,omitempty"`
}

type RendererCapabilities struct {
	SceneVersion string              `json:"scene_version"`
	Canvas       map[string]int      `json:"canvas"`
	Fonts        []FontAlias         `json:"fonts"`
	Primitives   []PrimitiveMetadata `json:"primitives"`
	Features     []string            `json:"features"`
}

func Capabilities() RendererCapabilities {
	return RendererCapabilities{
		SceneVersion: SceneVersionV1,
		Canvas:       map[string]int{"width": CanvasWidth, "height": CanvasHeight},
		Fonts:        FontAliases(),
		Primitives:   PrimitiveList(),
		Features: []string{
			"draw_ops",
			"source_mapped_issues",
			"report_detail",
			"image_png",
			"image_jpeg",
			"image_fit_exact_contain_cover_stretch",
			"rule",
			"card",
			"grid",
			"list",
			"stack",
			"section",
		},
	}
}

func PrimitiveList() []PrimitiveMetadata {
	return []PrimitiveMetadata{
		{
			Type:     NodeTypeText,
			Required: []string{"id", "type", "box", "text", "font.size"},
			Fields: map[string]any{
				"box":             "{x,y,w,h} in portrait pixels; must be fully inside 1440x2560",
				"text":            "string",
				"font.family":     []FontFamily{FontFamilySans, FontFamilySerif, FontFamilyMono},
				"font.size":       "positive integer pixels",
				"font.weight":     []FontWeight{FontWeightRegular, FontWeightBold},
				"color":           "#rrggbb; default #000000",
				"align":           []Align{AlignLeft, AlignCenter, AlignRight},
				"valign":          []VAlign{VAlignTop, VAlignMiddle, VAlignBottom},
				"wrap":            "boolean; default false",
				"max_lines":       "optional positive integer when wrap=true",
				"shrink_to_fit":   "boolean; default false",
				"shrink_min_size": "positive integer; default 12 when shrink_to_fit=true",
				"line_height":     "positive number; default 1.2",
			},
			Notes: []string{"Text overflow is an error unless shrink_to_fit=true succeeds."},
		},
		{
			Type:     NodeTypeRect,
			Required: []string{"id", "type", "box"},
			Fields: map[string]any{
				"box":          "{x,y,w,h} in portrait pixels; must be fully inside 1440x2560",
				"fill":         "#rrggbb; optional",
				"stroke":       "#rrggbb; optional",
				"stroke_width": "non-negative integer pixels; default 0 when stroke is absent, 1 when stroke is present",
				"radius":       "non-negative integer pixels; optional corner radius",
			},
		},
		{
			Type:     NodeTypeLine,
			Required: []string{"id", "type", "box", "from", "to"},
			Fields: map[string]any{
				"box":          "{x,y,w,h} in portrait pixels; bounds for the line and stroke",
				"from":         "{x,y} endpoint in integer pixels relative to box origin",
				"to":           "{x,y} endpoint in integer pixels relative to box origin",
				"stroke":       "#rrggbb; default #000000",
				"stroke_width": "positive integer pixels; default 1",
			},
			Notes: []string{"Line endpoints and the full stroke must fit inside the node box; renderer v1 fails rather than clipping."},
		},
		{
			Type:     NodeTypeImage,
			Required: []string{"id", "type", "box", "src"},
			Fields: map[string]any{
				"box":        "{x,y,w,h} in portrait pixels; must be fully inside 1440x2560",
				"src":        "local PNG or JPEG path; relative paths resolve against --asset-root or scene directory",
				"fit":        []ImageFit{ImageFitExact, ImageFitContain, ImageFitCover, ImageFitStretch},
				"position":   "center/top/bottom/left/right or {x,y} in [0,1]; default center",
				"background": "#rrggbb used for transparent pixels and contain padding; default canvas background",
			},
			Notes: []string{"fit=exact preserves the original exact-size behavior; contain/cover/stretch use deterministic scaling and are reported in draw_ops."},
		},
		{
			Type:     NodeTypeRule,
			Required: []string{"id", "type", "box"},
			Fields: map[string]any{
				"box":          "{x,y,w,h}; bounds for the divider",
				"orientation":  []string{"horizontal", "vertical"},
				"stroke":       "#rrggbb; default #000000",
				"stroke_width": "positive integer pixels; default 1",
				"inset_start":  "non-negative integer; default 0",
				"inset_end":    "non-negative integer; default 0",
			},
			Notes: []string{"Expands to one generated line draw op."},
		},
		{
			Type:     NodeTypeCard,
			Required: []string{"id", "type", "box"},
			Fields: map[string]any{
				"box":                     "{x,y,w,h}; full card box",
				"fill":                    "#rrggbb; default #f4f4f4",
				"stroke":                  "#rrggbb; default #111111",
				"stroke_width":            "non-negative integer; default 2",
				"radius":                  "non-negative integer; default 16",
				"padding":                 "non-negative integer; default 20",
				"header_gap":              "non-negative integer; default 12",
				"header.text":             "required when header is present",
				"header.height":           "positive integer; default 56",
				"body.text":               "required when body is present",
				"header/body font fields": "font, color, align, valign, wrap, shrink_to_fit, shrink_min_size, line_height",
			},
			Notes: []string{"Expands to background/header/body rect/text draw ops."},
		},
		{
			Type:     NodeTypeGrid,
			Required: []string{"id", "type", "box", "columns", "children"},
			Fields: map[string]any{
				"box":      "{x,y,w,h}; grid bounds",
				"columns":  "positive integer",
				"rows":     "optional positive integer; defaults to ceil(children/columns)",
				"gap_x":    "non-negative integer; default 24",
				"gap_y":    "non-negative integer; default 24",
				"children": "non-empty array of nodes; child box must be omitted because grid assigns it",
			},
			Notes: []string{"Grid emits no draw ops directly; it assigns absolute boxes to children row-major."},
		},
		{
			Type:     NodeTypeList,
			Required: []string{"id", "type", "box", "items"},
			Fields: map[string]any{
				"box":             "{x,y,w,h}; list bounds",
				"items":           "non-empty array of non-empty strings",
				"marker":          []string{"bullet", "dash", "number", "check", "none"},
				"font":            "font fields; default sans 26 regular",
				"color":           "#rrggbb; default #000000",
				"line_height":     "positive number; default 1.2",
				"shrink_to_fit":   "boolean; default false",
				"shrink_min_size": "positive integer; default 12",
				"marker_gap":      "non-negative integer; default 12",
				"item_gap":        "non-negative integer; default 8",
			},
			Notes: []string{"List emits generated marker and item text draw ops with independently wrapped item text.", "marker=check draws a geometric check mark, so it does not depend on font glyph coverage."},
		},
		{
			Type:     NodeTypeStack,
			Required: []string{"id", "type", "box", "children"},
			Fields: map[string]any{
				"box":       "{x,y,w,h}; stack bounds",
				"direction": []string{"vertical", "horizontal"},
				"gap":       "non-negative integer; default 24",
				"children":  "non-empty array of wrapper objects; vertical uses {height,node}, horizontal uses {width,node}; child node omits box",
			},
			Notes: []string{"Stack emits no draw ops directly; it assigns absolute boxes to fixed-size children."},
		},
		{
			Type:     NodeTypeSection,
			Required: []string{"id", "type", "box", "title"},
			Fields: map[string]any{
				"box":               "{x,y,w,h}; section bounds",
				"title":             "section heading text",
				"title_font":        "font fields; default sans 42 bold",
				"title_color":       "#rrggbb; default #000000",
				"title_height":      "positive integer; default 72",
				"gap":               "non-negative integer; default 24",
				"rule":              "boolean; default true",
				"rule_stroke":       "#rrggbb; default #000000",
				"rule_stroke_width": "positive integer; default 2",
				"rule_height":       "positive integer; default 4",
				"children":          "optional vertical stack child wrappers {height,node}; child node omits box",
			},
			Notes: []string{"Section expands to title/rule draw ops and optionally assigns vertical child boxes."},
		},
	}
}

func DescribeMarkdown() string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "# Lucio scene v1\n\n")
	fmt.Fprintf(&b, "Canvas: `%dx%d` portrait pixels. Origin is top-left. Output is deterministic `gray8` PNG.\n\n", CanvasWidth, CanvasHeight)
	fmt.Fprintf(&b, "Scene version: `%s`\n\n", SceneVersionV1)
	fmt.Fprintf(&b, "## Rules\n\n")
	fmt.Fprintf(&b, "- Node order is paint order; later nodes draw over earlier nodes.\n")
	fmt.Fprintf(&b, "- Top-level nodes require `id`, `type`, and `box`; layout children require `id` and `type` but omit `box` because the parent assigns it. IDs must be globally unique.\n")
	fmt.Fprintf(&b, "- Boxes and drawable content must stay inside the canvas.\n")
	fmt.Fprintf(&b, "- Unknown fields, bad enum values, clipping, overflow, and bad assets are errors by default.\n")
	fmt.Fprintf(&b, "- Colors are `#rrggbb` and convert to gray with `(299*R + 587*G + 114*B + 500) / 1000`.\n")
	fmt.Fprintf(&b, "- Image assets are local PNG/JPEG files; `fit` defaults to `exact`, with `contain`, `cover`, and `stretch` available.\n")
	fmt.Fprintf(&b, "- Use `lucio version --format json` to check renderer capabilities on the machine that will render/push.\n")
	fmt.Fprintf(&b, "- Normal loop: `lucio describe` or `lucio init` -> edit `scene.json` -> `lucio render --out --report` -> revise.\n\n")
	fmt.Fprintf(&b, "## Pipeline and reports\n\n")
	fmt.Fprintf(&b, "- Author nodes are the scene nodes you write; the renderer resolves them, emits a flat display list of core draw ops, then renders those draw ops.\n")
	fmt.Fprintf(&b, "- Default reports stay node-focused for readability. Use `--report-detail` on `render` or `push` to include `draw_ops` and source-mapping detail.\n\n")
	fmt.Fprintf(&b, "## Starter templates\n\n")
	for _, name := range TemplateNames() {
		fmt.Fprintf(&b, "- `%s`\n", name)
	}
	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "## Font aliases\n\n")
	for _, alias := range FontAliases() {
		fmt.Fprintf(&b, "- `%s`: %s / %s\n", alias.Family, alias.RegularFace, alias.BoldFace)
	}
	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "## Primitives\n\n")
	for _, primitive := range PrimitiveList() {
		fmt.Fprintf(&b, "### `%s`\n\n", primitive.Type)
		fmt.Fprintf(&b, "Required: `%v`\n\n", primitive.Required)
		for _, note := range primitive.Notes {
			fmt.Fprintf(&b, "- %s\n", note)
		}
		if len(primitive.Notes) > 0 {
			fmt.Fprintf(&b, "\n")
		}
	}
	fmt.Fprintf(&b, "## Complete tiny scene\n\n")
	fmt.Fprintf(&b, "```json\n%s\n```\n", tinyExamplesJSON())
	fmt.Fprintf(&b, "\n## Common mistakes\n\n")
	fmt.Fprintf(&b, "- Using `width`/`height` other than `1440`/`2560`.\n")
	fmt.Fprintf(&b, "- Using image assets whose decoded size differs from the node box while `fit` is still `exact`.\n")
	fmt.Fprintf(&b, "- Letting text overflow instead of setting `wrap` or `shrink_to_fit`.\n")
	fmt.Fprintf(&b, "- Adding `box` to `grid.children[]`; grid children must let the grid assign their boxes.\n")
	fmt.Fprintf(&b, "- Using URLs, `data:` URIs, system font names, or color names like `red`.\n")
	return b.String()
}

func PrimitivesMarkdown() string {
	var b bytes.Buffer
	for _, primitive := range PrimitiveList() {
		fmt.Fprintf(&b, "## %s\n\n", primitive.Type)
		fmt.Fprintf(&b, "Required: `%v`\n\n", primitive.Required)
		keys := make([]string, 0, len(primitive.Fields))
		for name := range primitive.Fields {
			keys = append(keys, name)
		}
		sort.Strings(keys)
		for _, name := range keys {
			fmt.Fprintf(&b, "- `%s`: %v\n", name, primitive.Fields[name])
		}
		for _, note := range primitive.Notes {
			fmt.Fprintf(&b, "- Note: %s\n", note)
		}
		fmt.Fprintf(&b, "\n")
	}
	return b.String()
}

func tinyExamplesJSON() string {
	scene := Scene{
		Schema:  SchemaURLV1,
		Version: SceneVersionV1,
		Canvas:  Canvas{Width: CanvasWidth, Height: CanvasHeight, Background: DefaultCanvasBackground},
		Nodes: []Node{
			{
				ID:     "title",
				Type:   NodeTypeText,
				Box:    Box{X: 80, Y: 80, W: 1280, H: 120},
				Text:   "Hello Lucio",
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
				ID:          "rule",
				Type:        NodeTypeLine,
				Box:         Box{X: 80, Y: 640, W: 1280, H: 8},
				From:        &Point{X: 0, Y: 4},
				To:          &Point{X: 1279, Y: 4},
				Stroke:      DefaultTextColor,
				StrokeWidth: intPtr(3),
			},
			{
				ID:   "newspaper",
				Type: NodeTypeImage,
				Box:  Box{X: 0, Y: 0, W: CanvasWidth, H: CanvasHeight},
				Src:  "today-newspaper-1440x2560.png",
			},
		},
	}
	buf, err := json.MarshalIndent(scene, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(buf)
}

func intPtr(v int) *int { return &v }
