package renderer

const (
	SceneVersionV1 = "lucio.scene/v1"
	SchemaURLV1    = "https://raw.githubusercontent.com/pablasso/lucio/main/schemas/lucio.scene.v1.json"

	CanvasWidth  = 1440
	CanvasHeight = 2560
)

type NodeType string

const (
	NodeTypeText    NodeType = "text"
	NodeTypeRect    NodeType = "rect"
	NodeTypeLine    NodeType = "line"
	NodeTypeImage   NodeType = "image"
	NodeTypeRule    NodeType = "rule"
	NodeTypeCard    NodeType = "card"
	NodeTypeGrid    NodeType = "grid"
	NodeTypeList    NodeType = "list"
	NodeTypeStack   NodeType = "stack"
	NodeTypeSection NodeType = "section"
)

type FontFamily string

const (
	FontFamilySans  FontFamily = "sans"
	FontFamilySerif FontFamily = "serif"
	FontFamilyMono  FontFamily = "mono"
)

type FontWeight string

const (
	FontWeightRegular FontWeight = "regular"
	FontWeightBold    FontWeight = "bold"
)

type Align string

const (
	AlignLeft   Align = "left"
	AlignCenter Align = "center"
	AlignRight  Align = "right"
)

type VAlign string

const (
	VAlignTop    VAlign = "top"
	VAlignMiddle VAlign = "middle"
	VAlignBottom VAlign = "bottom"
)

// Scene is a convenience builder for basic text/rect/line/image JSON templates.
// Parsing uses AuthorScene, which also represents every higher-level primitive.
// The supported client contract is JSON, not these internal Go builder types.
type Scene struct {
	Schema  string `json:"$schema,omitempty"`
	Version string `json:"version"`
	Canvas  Canvas `json:"canvas,omitempty"`
	Nodes   []Node `json:"nodes"`
}

type Canvas struct {
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	Background string `json:"background,omitempty"`
}

type Box struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

type Point struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type Font struct {
	Family FontFamily `json:"family,omitempty"`
	Size   int        `json:"size"`
	Weight FontWeight `json:"weight,omitempty"`
}

// Node is a tagged union keyed by Type. Fields not meaningful for a node type
// are rejected by strict validation in the renderer core.
type Node struct {
	ID   string   `json:"id"`
	Type NodeType `json:"type"`
	Box  Box      `json:"box"`

	// text fields
	Text          string   `json:"text,omitempty"`
	Font          *Font    `json:"font,omitempty"`
	Align         Align    `json:"align,omitempty"`
	VAlign        VAlign   `json:"valign,omitempty"`
	Color         string   `json:"color,omitempty"`
	Wrap          *bool    `json:"wrap,omitempty"`
	MaxLines      *int     `json:"max_lines,omitempty"`
	ShrinkToFit   *bool    `json:"shrink_to_fit,omitempty"`
	ShrinkMinSize *int     `json:"shrink_min_size,omitempty"`
	LineHeight    *float64 `json:"line_height,omitempty"`

	// rect fields
	Fill        string `json:"fill,omitempty"`
	Stroke      string `json:"stroke,omitempty"`
	StrokeWidth *int   `json:"stroke_width,omitempty"`
	Radius      *int   `json:"radius,omitempty"`

	// line fields. Points are integer pixels relative to Box.X/Box.Y.
	From *Point `json:"from,omitempty"`
	To   *Point `json:"to,omitempty"`

	// image fields
	Src string `json:"src,omitempty"`
}
