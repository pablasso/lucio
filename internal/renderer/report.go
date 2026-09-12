package renderer

type Severity string

const (
	SeverityNotice  Severity = "notice"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

type Report struct {
	OK           bool               `json:"ok"`
	SceneVersion string             `json:"scene_version"`
	Output       OutputMetadata     `json:"output,omitempty"`
	Issues       []Issue            `json:"issues"`
	Nodes        []NodeRenderInfo   `json:"nodes"`
	DrawOps      []DrawOpRenderInfo `json:"draw_ops,omitempty"`
}

type OutputMetadata struct {
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Format string `json:"format"`
	Color  string `json:"color"`
	SHA256 string `json:"sha256,omitempty"`
	Path   string `json:"path,omitempty"`
}

type Issue struct {
	Severity Severity `json:"severity"`
	Node     string   `json:"node,omitempty"`
	DrawOp   string   `json:"draw_op,omitempty"`
	Path     string   `json:"path"`
	Code     string   `json:"code"`
	Message  string   `json:"message"`
}

type NodeRenderInfo struct {
	ID             string            `json:"id"`
	Type           string            `json:"type"`
	Path           string            `json:"path,omitempty"`
	ParentID       string            `json:"parent_id,omitempty"`
	BoxSource      string            `json:"box_source,omitempty"`
	Box            Box               `json:"box"`
	FinalBox       Box               `json:"final_box"`
	DrawOps        []string          `json:"draw_ops,omitempty"`
	Layout         *NodeLayoutReport `json:"layout,omitempty"`
	ActualFontSize *int              `json:"actual_font_size,omitempty"`
	Lines          *int              `json:"lines,omitempty"`
}

// NodeLayoutReport describes the concrete layout of higher-level primitives.
type NodeLayoutReport struct {
	Grid    *GridLayoutReport    `json:"grid,omitempty"`
	Image   *ImageFitReport      `json:"image,omitempty"`
	List    *ListLayoutReport    `json:"list,omitempty"`
	Stack   *StackLayoutReport   `json:"stack,omitempty"`
	Section *SectionLayoutReport `json:"section,omitempty"`
}

type GridLayoutReport struct {
	Columns     int                     `json:"columns"`
	Rows        int                     `json:"rows"`
	GapX        int                     `json:"gap_x"`
	GapY        int                     `json:"gap_y"`
	CellWidths  []int                   `json:"cell_widths,omitempty"`
	CellHeights []int                   `json:"cell_heights,omitempty"`
	Children    []GridChildLayoutReport `json:"children,omitempty"`
}

type GridChildLayoutReport struct {
	ID   string      `json:"id"`
	Cell GridCellRef `json:"cell"`
	Box  Box         `json:"box"`
}

type GridCellRef struct {
	Row    int `json:"row"`
	Column int `json:"column"`
}

type ImageFitReport struct {
	SourcePath     string `json:"source_path,omitempty"`
	SourceWidth    int    `json:"source_width,omitempty"`
	SourceHeight   int    `json:"source_height,omitempty"`
	Fit            string `json:"fit,omitempty"`
	FinalDrawRect  *Box   `json:"final_draw_rect,omitempty"`
	ScaledSize     *Size  `json:"scaled_size,omitempty"`
	ScaledCropRect *Box   `json:"scaled_crop_rect,omitempty"`
	SourceCropRect *Box   `json:"source_crop_rect,omitempty"`
	Background     string `json:"background,omitempty"`
}

type Size struct {
	W int `json:"w"`
	H int `json:"h"`
}

type ListLayoutReport struct {
	Marker            string                 `json:"marker"`
	ItemCount         int                    `json:"item_count"`
	FontSize          int                    `json:"font_size"`
	MarkerColumnWidth int                    `json:"marker_column_width"`
	MarkerGap         int                    `json:"marker_gap"`
	ItemGap           int                    `json:"item_gap"`
	Items             []ListItemLayoutReport `json:"items,omitempty"`
}

type ListItemLayoutReport struct {
	Index     int    `json:"index"`
	Marker    string `json:"marker,omitempty"`
	MarkerBox *Box   `json:"marker_box,omitempty"`
	TextBox   Box    `json:"text_box"`
	Lines     int    `json:"lines"`
}

type StackLayoutReport struct {
	Direction string                   `json:"direction"`
	Gap       int                      `json:"gap"`
	Children  []StackChildLayoutReport `json:"children,omitempty"`
}

type StackChildLayoutReport struct {
	ID   string `json:"id"`
	Size int    `json:"size"`
	Box  Box    `json:"box"`
}

type SectionLayoutReport struct {
	TitleBox   *Box                     `json:"title_box,omitempty"`
	RuleBox    *Box                     `json:"rule_box,omitempty"`
	ContentBox *Box                     `json:"content_box,omitempty"`
	Gap        int                      `json:"gap"`
	Children   []StackChildLayoutReport `json:"children,omitempty"`
}

type DrawOpRenderInfo struct {
	ID             string          `json:"id"`
	Type           string          `json:"type"`
	SourceID       string          `json:"source_id"`
	SourceType     string          `json:"source_type"`
	SourcePath     string          `json:"source_path"`
	Role           string          `json:"role,omitempty"`
	Box            Box             `json:"box"`
	FinalBox       Box             `json:"final_box"`
	ActualFontSize *int            `json:"actual_font_size,omitempty"`
	Lines          *int            `json:"lines,omitempty"`
	Image          *ImageFitReport `json:"image,omitempty"`
}

func EmptyReport(version string) Report {
	return Report{
		OK:           true,
		SceneVersion: version,
		Output: OutputMetadata{
			Width:  CanvasWidth,
			Height: CanvasHeight,
			Format: "png",
			Color:  "gray8",
		},
		Issues: []Issue{},
		Nodes:  []NodeRenderInfo{},
	}
}

func NewValidationReport(scene AuthorScene, issues []Issue) Report {
	report := EmptyReport(scene.Version)
	if issues == nil {
		issues = []Issue{}
	}
	report.Issues = issues
	report.OK = !HasErrors(issues)
	for _, node := range scene.Nodes {
		if node.ID == "" && node.Type == "" {
			continue
		}
		box := Box{}
		if node.Box != nil {
			box = *node.Box
		}
		report.Nodes = append(report.Nodes, NodeRenderInfo{
			ID: node.ID, Type: node.Type, Box: box, FinalBox: box,
		})
	}
	return report
}

func NewCompiledReport(compiled CompiledScene, issues []Issue, detail bool) Report {
	report := EmptyReport(compiled.Author.Version)
	if issues == nil {
		issues = []Issue{}
	}
	report.Issues = issues
	report.OK = !HasErrors(issues)

	richNodes := detail || compiledNeedsRichNodeReport(compiled)
	for _, node := range compiled.Resolved.Nodes {
		if node.ID == "" && node.Type == "" {
			continue
		}
		info := NodeRenderInfo{
			ID:       node.ID,
			Type:     node.Type,
			Box:      node.Box,
			FinalBox: node.FinalBox,
		}
		if richNodes {
			info.Path = node.Path
			info.ParentID = node.ParentID
			info.BoxSource = node.BoxSource
			info.DrawOps = append([]string(nil), node.DrawOps...)
			info.Layout = node.Layout
		}
		report.Nodes = append(report.Nodes, info)
	}
	return report
}

func compiledNeedsRichNodeReport(compiled CompiledScene) bool {
	if authorNeedsRichReport(compiled.Author.Nodes) {
		return true
	}
	for _, node := range compiled.Resolved.Nodes {
		if node.ParentID != "" || node.BoxSource == BoxSourceAssigned || len(node.Children) > 0 || node.Layout != nil || !isLowLevelNodeType(node.Type) {
			return true
		}
	}
	return false
}

func authorNeedsRichReport(nodes []AuthorNode) bool {
	for _, node := range nodes {
		if node.Image != nil && node.Image.Fit != "" && node.Image.Fit != ImageFitExact {
			return true
		}
		if node.Grid != nil && authorNeedsRichReport(node.Grid.Children) {
			return true
		}
	}
	return false
}

func setTextNodeRenderInfoByID(report *Report, id string, fontSize, lines int) {
	for i := range report.Nodes {
		if report.Nodes[i].ID == id {
			report.Nodes[i].ActualFontSize = &fontSize
			report.Nodes[i].Lines = &lines
			return
		}
	}
}

func buildDrawOpReports(displayList DisplayList, metrics map[string]drawOpRenderMetrics) []DrawOpRenderInfo {
	infos := make([]DrawOpRenderInfo, 0, len(displayList.Ops))
	for _, op := range displayList.Ops {
		box := op.Command.box()
		info := DrawOpRenderInfo{
			ID:         op.ID,
			Type:       op.Command.typeName(),
			SourceID:   op.Source.ID,
			SourceType: op.Source.Type,
			SourcePath: op.Source.Path,
			Role:       op.Role,
			Box:        box,
			FinalBox:   box,
		}
		if metric, ok := metrics[op.ID]; ok {
			if metric.FinalBox != (Box{}) {
				info.FinalBox = metric.FinalBox
			}
			info.ActualFontSize = cloneIntPtr(metric.ActualFontSize)
			info.Lines = cloneIntPtr(metric.Lines)
			info.Image = metric.Image
		}
		infos = append(infos, info)
	}
	return infos
}

func HasErrors(issues []Issue) bool {
	for _, issue := range issues {
		if issue.Severity == SeverityError {
			return true
		}
	}
	return false
}

func CountErrors(issues []Issue) int {
	n := 0
	for _, issue := range issues {
		if issue.Severity == SeverityError {
			n++
		}
	}
	return n
}
