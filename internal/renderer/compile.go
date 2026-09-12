package renderer

import "fmt"

const (
	BoxSourceExplicit = "explicit"
	BoxSourceAssigned = "assigned"
)

// AuthorScene is the complete parsed author-facing scene, including nested
// primitives and their source paths. It is the sole parsed scene representation.
type AuthorScene struct {
	Schema  string
	Version string
	Canvas  Canvas
	Nodes   []AuthorNode
}

// AuthorNode represents one node exactly as authored, with a stable JSON path.
type AuthorNode struct {
	ID      string
	Type    string
	Path    string
	Box     *Box
	BoxPath string

	Text  *TextSpec
	Rect  *RectSpec
	Line  *LineSpec
	Image *ImageSpec

	Rule    *RuleSpec
	Card    *CardSpec
	Grid    *GridSpec
	List    *ListSpec
	Stack   *StackSpec
	Section *SectionSpec
}

type TextSpec struct {
	Text          string
	Font          *Font
	Align         Align
	VAlign        VAlign
	Color         string
	Wrap          *bool
	MaxLines      *int
	ShrinkToFit   *bool
	ShrinkMinSize *int
	LineHeight    *float64
}

type RectSpec struct {
	Fill        string
	Stroke      string
	StrokeWidth *int
	Radius      *int
}

type LineSpec struct {
	From        *Point
	To          *Point
	Stroke      string
	StrokeWidth *int
}

type ImageSpec struct {
	Src        string
	Fit        ImageFit
	Position   ImagePosition
	Background string
}

type ImageFit string

const (
	ImageFitExact   ImageFit = "exact"
	ImageFitContain ImageFit = "contain"
	ImageFitCover   ImageFit = "cover"
	ImageFitStretch ImageFit = "stretch"
)

type ImagePosition struct {
	X float64
	Y float64
}

type RuleSpec struct {
	Orientation string
	Stroke      string
	StrokeWidth int
	InsetStart  int
	InsetEnd    int
}

type CardSpec struct {
	Fill        string
	Stroke      string
	StrokeWidth int
	Radius      int
	Padding     int
	HeaderGap   int
	Header      *CardTextSpec
	Body        *CardTextSpec
}

type CardTextSpec struct {
	Text          string
	Height        int
	Fill          string
	Font          Font
	Color         string
	Align         Align
	VAlign        VAlign
	Wrap          bool
	MaxLines      *int
	ShrinkToFit   bool
	ShrinkMinSize *int
	LineHeight    *float64
}

type GridSpec struct {
	Columns  int
	Rows     *int
	GapX     int
	GapY     int
	Children []AuthorNode
}

type ListSpec struct {
	Items         []string
	Marker        string
	Font          Font
	Color         string
	LineHeight    *float64
	ShrinkToFit   bool
	ShrinkMinSize int
	MarkerGap     int
	ItemGap       int
}

type StackSpec struct {
	Direction string
	Gap       int
	Children  []StackChildSpec
}

type StackChildSpec struct {
	Path string
	Size int
	Node AuthorNode
}

type SectionSpec struct {
	Title           string
	TitleFont       Font
	TitleColor      string
	TitleAlign      Align
	TitleVAlign     VAlign
	TitleHeight     int
	Gap             int
	Rule            bool
	RuleStroke      string
	RuleStrokeWidth int
	RuleHeight      int
	Children        []StackChildSpec
}

// ResolvedScene is the author scene after defaults/layout assign concrete
// absolute boxes. Low-level nodes resolve 1:1; containers assign child boxes.
type ResolvedScene struct {
	Author  AuthorScene
	Nodes   []ResolvedNode
	RootIDs []string
}

type ResolvedNode struct {
	ID        string
	Type      string
	Path      string
	Box       Box
	FinalBox  Box
	BoxSource string

	ParentID string
	Children []string
	DrawOps  []string

	Layout *NodeLayoutReport
}

// DisplayList is the flat, ordered list of core draw operations the renderer
// knows how to draw.
type DisplayList struct {
	Ops []DrawOp
}

type DrawOp struct {
	ID      string
	Command CoreCommand
	Source  SourceRef
	Role    string

	// Generated is true for ops synthesized from a higher-level author node.
	// Low-level 1:1 ops deliberately keep Generated=false even though their ID
	// equals the author node ID.
	Generated bool
}

type CoreCommand struct {
	Rect  *CoreRectCommand
	Text  *CoreTextCommand
	Line  *CoreLineCommand
	Image *CoreImageCommand
}

type CoreRectCommand struct {
	Box         Box
	Fill        string
	Stroke      string
	StrokeWidth *int
	Radius      *int
}

type CoreTextCommand struct {
	Box           Box
	Text          string
	Font          *Font
	Align         Align
	VAlign        VAlign
	Color         string
	Wrap          *bool
	MaxLines      *int
	ShrinkToFit   *bool
	ShrinkMinSize *int
	LineHeight    *float64
}

type CoreLineCommand struct {
	Box         Box
	From        *Point
	To          *Point
	Stroke      string
	StrokeWidth *int
}

type CoreImageCommand struct {
	Box        Box
	Src        string
	Fit        ImageFit
	Position   ImagePosition
	Background string
}

type SourceRef struct {
	ID        string
	Type      string
	Path      string
	FieldPath map[string]string
}

type CompiledScene struct {
	Author      AuthorScene
	Resolved    ResolvedScene
	DisplayList DisplayList
}

// CompileSceneJSON parses, validates, resolves, and emits the display list for
// scene JSON. It is intentionally usable in tests and future tooling without
// producing pixels.
func CompileSceneJSON(data []byte) (CompiledScene, Report) {
	return compileSceneJSON(data, false)
}

func compileSceneJSON(data []byte, reportDetail bool) (CompiledScene, Report) {
	author, validationIssues := parseAuthorSceneStrict(data)
	compiled := CompiledScene{Author: author}
	validationReport := NewValidationReport(author, validationIssues)

	if HasErrors(validationReport.Issues) {
		return compiled, validationReport
	}

	resolved, resolveIssues := resolveAuthorScene(compiled.Author)
	compiled.Resolved = resolved
	if HasErrors(resolveIssues) {
		return compiled, NewCompiledReport(compiled, append(validationReport.Issues, resolveIssues...), reportDetail)
	}

	displayList, displayIssues := emitDisplayList(&resolved)
	compiled.Resolved = resolved
	compiled.DisplayList = displayList
	issues := append(validationReport.Issues, resolveIssues...)
	issues = append(issues, displayIssues...)
	return compiled, NewCompiledReport(compiled, issues, reportDetail)
}

func resolveAuthorScene(author AuthorScene) (ResolvedScene, []Issue) {
	resolved := ResolvedScene{
		Author:  author,
		Nodes:   make([]ResolvedNode, 0, countAuthorNodes(author.Nodes)),
		RootIDs: make([]string, 0, len(author.Nodes)),
	}
	var issues []Issue
	for _, node := range author.Nodes {
		if node.Box == nil {
			issues = append(issues, errorIssue(node.ID, node.Path+"/box", "missing_required", "top-level node box is required"))
			continue
		}
		resolved.RootIDs = append(resolved.RootIDs, node.ID)
		issues = append(issues, resolveAuthorNode(node, *node.Box, BoxSourceExplicit, "", &resolved)...)
	}
	return resolved, issues
}

func countAuthorNodes(nodes []AuthorNode) int {
	n := 0
	for _, node := range nodes {
		n++
		if node.Grid != nil {
			n += countAuthorNodes(node.Grid.Children)
		}
		if node.Stack != nil {
			n += countStackChildNodes(node.Stack.Children)
		}
		if node.Section != nil {
			n += countStackChildNodes(node.Section.Children)
		}
	}
	return n
}

func countStackChildNodes(children []StackChildSpec) int {
	n := 0
	for _, child := range children {
		n += countAuthorNodes([]AuthorNode{child.Node})
	}
	return n
}

func resolveAuthorNode(author AuthorNode, box Box, boxSource, parentID string, resolved *ResolvedScene) []Issue {
	idx := len(resolved.Nodes)
	resolved.Nodes = append(resolved.Nodes, ResolvedNode{
		ID:        author.ID,
		Type:      author.Type,
		Path:      author.Path,
		Box:       box,
		FinalBox:  box,
		BoxSource: boxSource,
		ParentID:  parentID,
	})

	var issues []Issue
	switch NodeType(author.Type) {
	case NodeTypeLine:
		issues = append(issues, validateResolvedLine(author, box)...)
	case NodeTypeRect:
		issues = append(issues, validateResolvedRect(author, box)...)
	case NodeTypeRule:
		issues = append(issues, validateResolvedRule(author, box)...)
	case NodeTypeCard:
		issues = append(issues, validateResolvedCard(author, box)...)
	case NodeTypeGrid:
		childIDs, layout, gridIssues := resolveGridChildren(author, box, resolved)
		issues = append(issues, gridIssues...)
		resolved.Nodes[idx].Children = childIDs
		if layout != nil {
			resolved.Nodes[idx].Layout = &NodeLayoutReport{Grid: layout}
		}
	case NodeTypeList:
		layout, _, listIssues := layoutList(author, box)
		issues = append(issues, listIssues...)
		if layout != nil {
			resolved.Nodes[idx].Layout = &NodeLayoutReport{List: layout}
		}
	case NodeTypeStack:
		childIDs, layout, stackIssues := resolveStackChildren(author, box, resolved)
		issues = append(issues, stackIssues...)
		resolved.Nodes[idx].Children = childIDs
		if layout != nil {
			resolved.Nodes[idx].Layout = &NodeLayoutReport{Stack: layout}
		}
	case NodeTypeSection:
		childIDs, layout, sectionIssues := resolveSection(author, box, resolved)
		issues = append(issues, sectionIssues...)
		resolved.Nodes[idx].Children = childIDs
		if layout != nil {
			resolved.Nodes[idx].Layout = &NodeLayoutReport{Section: layout}
		}
	}
	return issues
}

func resolveGridChildren(author AuthorNode, box Box, resolved *ResolvedScene) ([]string, *GridLayoutReport, []Issue) {
	if author.Grid == nil {
		return nil, nil, []Issue{errorIssue(author.ID, author.Path, "internal_compile_error", "grid author node is missing grid spec")}
	}
	grid := author.Grid
	var issues []Issue
	childCount := len(grid.Children)
	rows := 0
	if grid.Rows != nil {
		rows = *grid.Rows
	} else if grid.Columns > 0 && childCount > 0 {
		rows = ceilDiv(childCount, grid.Columns)
	}
	layout := &GridLayoutReport{Columns: grid.Columns, Rows: rows, GapX: grid.GapX, GapY: grid.GapY}
	if grid.Columns <= 0 || rows <= 0 || childCount == 0 {
		return nil, layout, issues
	}
	if rows*grid.Columns < childCount {
		issues = append(issues, errorIssue(author.ID, author.Path+"/rows", "out_of_range", "rows * columns must be at least the number of grid children"))
		return nil, layout, issues
	}
	availableW := box.W - grid.GapX*(grid.Columns-1)
	availableH := box.H - grid.GapY*(rows-1)
	if availableW <= 0 {
		issues = append(issues, errorIssue(author.ID, author.Path+"/gap_x", "out_of_range", "grid gap_x leaves no positive cell width"))
		return nil, layout, issues
	}
	if availableH <= 0 {
		issues = append(issues, errorIssue(author.ID, author.Path+"/gap_y", "out_of_range", "grid gap_y leaves no positive cell height"))
		return nil, layout, issues
	}
	widths := distributePixels(availableW, grid.Columns)
	heights := distributePixels(availableH, rows)
	for _, width := range widths {
		if width <= 0 {
			issues = append(issues, errorIssue(author.ID, author.Path+"/columns", "out_of_range", "computed grid cell widths must be positive"))
			return nil, layout, issues
		}
	}
	for _, height := range heights {
		if height <= 0 {
			issues = append(issues, errorIssue(author.ID, author.Path+"/rows", "out_of_range", "computed grid cell heights must be positive"))
			return nil, layout, issues
		}
	}
	layout.CellWidths = widths
	layout.CellHeights = heights

	xs := make([]int, grid.Columns)
	x := box.X
	for i, width := range widths {
		xs[i] = x
		x += width + grid.GapX
	}
	ys := make([]int, rows)
	y := box.Y
	for i, height := range heights {
		ys[i] = y
		y += height + grid.GapY
	}

	childIDs := make([]string, 0, childCount)
	for i, child := range grid.Children {
		row := i / grid.Columns
		col := i % grid.Columns
		childBox := Box{X: xs[col], Y: ys[row], W: widths[col], H: heights[row]}
		childIDs = append(childIDs, child.ID)
		layout.Children = append(layout.Children, GridChildLayoutReport{ID: child.ID, Cell: GridCellRef{Row: row, Column: col}, Box: childBox})
		issues = append(issues, resolveAuthorNode(child, childBox, BoxSourceAssigned, author.ID, resolved)...)
	}
	return childIDs, layout, issues
}

func validateResolvedLine(author AuthorNode, box Box) []Issue {
	if author.Line == nil {
		return nil
	}
	var issues []Issue
	if author.Line.From != nil {
		validatePointInsideBox(*author.Line.From, box, author.Path+"/from", author.ID, &issues)
	}
	if author.Line.To != nil {
		validatePointInsideBox(*author.Line.To, box, author.Path+"/to", author.ID, &issues)
	}
	return issues
}

func validateResolvedRect(author AuthorNode, box Box) []Issue {
	if author.Rect == nil || author.Rect.Radius == nil {
		return nil
	}
	radius := *author.Rect.Radius
	if radius > minInt(box.W, box.H)/2 {
		return []Issue{errorIssue(author.ID, author.Path+"/radius", "out_of_range", "radius must be at most half of the smaller box dimension")}
	}
	return nil
}

func validateResolvedRule(author AuthorNode, box Box) []Issue {
	if author.Rule == nil {
		return nil
	}
	rule := author.Rule
	if rule.Orientation == "vertical" {
		if box.W < rule.StrokeWidth {
			return []Issue{errorIssue(author.ID, author.Path+"/stroke_width", "out_of_range", "vertical rule box.w must be at least stroke_width")}
		}
		start := rule.InsetStart
		end := box.H - 1 - rule.InsetEnd
		if start > end {
			return []Issue{errorIssue(author.ID, author.Path+"/inset_start", "out_of_range", "rule insets must leave at least one drawable endpoint pair")}
		}
		return nil
	}
	if box.H < rule.StrokeWidth {
		return []Issue{errorIssue(author.ID, author.Path+"/stroke_width", "out_of_range", "horizontal rule box.h must be at least stroke_width")}
	}
	start := rule.InsetStart
	end := box.W - 1 - rule.InsetEnd
	if start > end {
		return []Issue{errorIssue(author.ID, author.Path+"/inset_start", "out_of_range", "rule insets must leave at least one drawable endpoint pair")}
	}
	return nil
}

func validateResolvedCard(author AuthorNode, box Box) []Issue {
	if author.Card == nil {
		return nil
	}
	card := author.Card
	var issues []Issue
	if card.Radius > minInt(box.W, box.H)/2 {
		issues = append(issues, errorIssue(author.ID, author.Path+"/radius", "out_of_range", "radius must be at most half of the smaller box dimension"))
	}
	if card.Header == nil && card.Body == nil {
		return issues
	}
	if card.Padding*2 >= box.W || card.Padding*2 >= box.H {
		issues = append(issues, errorIssue(author.ID, author.Path+"/padding", "out_of_range", "padding * 2 must be smaller than card box width and height when content exists"))
		return issues
	}
	contentW := box.W - 2*card.Padding
	contentH := box.H - 2*card.Padding
	if contentW <= 0 || contentH <= 0 {
		return issues
	}
	if card.Header != nil {
		if card.Header.Height <= 0 || card.Header.Height > contentH {
			issues = append(issues, errorIssue(author.ID, author.Path+"/header/height", "out_of_range", "header height must fit inside the card content area"))
			return issues
		}
	}
	if card.Body != nil {
		bodyH := contentH
		if card.Header != nil {
			bodyH = contentH - card.Header.Height - card.HeaderGap
		}
		if bodyH <= 0 {
			issues = append(issues, errorIssue(author.ID, author.Path+"/body", "out_of_range", "body area must be positive"))
		}
	}
	return issues
}

func distributePixels(total, parts int) []int {
	values := make([]int, parts)
	if parts <= 0 {
		return values
	}
	base := total / parts
	remainder := total % parts
	for i := 0; i < parts; i++ {
		values[i] = base
		if i < remainder {
			values[i]++
		}
	}
	return values
}

func ceilDiv(n, d int) int {
	if d <= 0 {
		return 0
	}
	return (n + d - 1) / d
}

func emitDisplayList(resolved *ResolvedScene) (DisplayList, []Issue) {
	authorsByID := make(map[string]AuthorNode, countAuthorNodes(resolved.Author.Nodes))
	authorIDs := make(map[string]bool, countAuthorNodes(resolved.Author.Nodes))
	collectAuthorNodesByID(resolved.Author.Nodes, authorsByID, authorIDs)

	list := DisplayList{Ops: make([]DrawOp, 0, len(resolved.Nodes))}
	var issues []Issue
	for i := range resolved.Nodes {
		rn := &resolved.Nodes[i]
		author, ok := authorsByID[rn.ID]
		if !ok {
			issues = append(issues, errorIssue(rn.ID, rn.Path, "internal_compile_error", "resolved node has no matching author node"))
			continue
		}
		ops, opIssues := drawOpsForAuthorNode(author, *rn)
		issues = append(issues, opIssues...)
		if len(ops) == 0 {
			continue
		}
		list.Ops = append(list.Ops, ops...)
		rn.DrawOps = drawOpIDs(ops)
	}
	issues = append(issues, validateDrawOpIDs(list, authorIDs)...)
	return list, issues
}

func collectAuthorNodesByID(nodes []AuthorNode, byID map[string]AuthorNode, ids map[string]bool) {
	for _, node := range nodes {
		byID[node.ID] = node
		ids[node.ID] = true
		if node.Grid != nil {
			collectAuthorNodesByID(node.Grid.Children, byID, ids)
		}
		if node.Stack != nil {
			collectStackChildrenByID(node.Stack.Children, byID, ids)
		}
		if node.Section != nil {
			collectStackChildrenByID(node.Section.Children, byID, ids)
		}
	}
}

func collectStackChildrenByID(children []StackChildSpec, byID map[string]AuthorNode, ids map[string]bool) {
	for _, child := range children {
		collectAuthorNodesByID([]AuthorNode{child.Node}, byID, ids)
	}
}

func drawOpIDs(ops []DrawOp) []string {
	ids := make([]string, 0, len(ops))
	for _, op := range ops {
		ids = append(ids, op.ID)
	}
	return ids
}

func drawOpsForAuthorNode(author AuthorNode, resolved ResolvedNode) ([]DrawOp, []Issue) {
	source := sourceRefForAuthorNode(author)
	switch NodeType(author.Type) {
	case NodeTypeText:
		if author.Text == nil {
			return nil, []Issue{errorIssue(author.ID, author.Path, "internal_compile_error", "text author node is missing text spec")}
		}
		return []DrawOp{{
			ID:     author.ID,
			Role:   string(NodeTypeText),
			Source: source,
			Command: CoreCommand{Text: &CoreTextCommand{
				Box:           resolved.Box,
				Text:          author.Text.Text,
				Font:          cloneFontPtr(author.Text.Font),
				Align:         author.Text.Align,
				VAlign:        author.Text.VAlign,
				Color:         author.Text.Color,
				Wrap:          cloneBoolPtr(author.Text.Wrap),
				MaxLines:      cloneIntPtr(author.Text.MaxLines),
				ShrinkToFit:   cloneBoolPtr(author.Text.ShrinkToFit),
				ShrinkMinSize: cloneIntPtr(author.Text.ShrinkMinSize),
				LineHeight:    cloneFloatPtr(author.Text.LineHeight),
			}},
		}}, nil
	case NodeTypeRect:
		if author.Rect == nil {
			return nil, []Issue{errorIssue(author.ID, author.Path, "internal_compile_error", "rect author node is missing rect spec")}
		}
		return []DrawOp{{
			ID:     author.ID,
			Role:   string(NodeTypeRect),
			Source: source,
			Command: CoreCommand{Rect: &CoreRectCommand{
				Box:         resolved.Box,
				Fill:        author.Rect.Fill,
				Stroke:      author.Rect.Stroke,
				StrokeWidth: cloneIntPtr(author.Rect.StrokeWidth),
				Radius:      cloneIntPtr(author.Rect.Radius),
			}},
		}}, nil
	case NodeTypeLine:
		if author.Line == nil {
			return nil, []Issue{errorIssue(author.ID, author.Path, "internal_compile_error", "line author node is missing line spec")}
		}
		return []DrawOp{{
			ID:     author.ID,
			Role:   string(NodeTypeLine),
			Source: source,
			Command: CoreCommand{Line: &CoreLineCommand{
				Box:         resolved.Box,
				From:        clonePointPtr(author.Line.From),
				To:          clonePointPtr(author.Line.To),
				Stroke:      author.Line.Stroke,
				StrokeWidth: cloneIntPtr(author.Line.StrokeWidth),
			}},
		}}, nil
	case NodeTypeImage:
		if author.Image == nil {
			return nil, []Issue{errorIssue(author.ID, author.Path, "internal_compile_error", "image author node is missing image spec")}
		}
		return []DrawOp{{
			ID:     author.ID,
			Role:   string(NodeTypeImage),
			Source: source,
			Command: CoreCommand{Image: &CoreImageCommand{
				Box:        resolved.Box,
				Src:        author.Image.Src,
				Fit:        author.Image.Fit,
				Position:   author.Image.Position,
				Background: author.Image.Background,
			}},
		}}, nil
	case NodeTypeRule:
		return drawOpsForRule(author, resolved)
	case NodeTypeCard:
		return drawOpsForCard(author, resolved)
	case NodeTypeGrid:
		return nil, nil
	case NodeTypeList:
		return drawOpsForList(author, resolved)
	case NodeTypeStack:
		return nil, nil
	case NodeTypeSection:
		return drawOpsForSection(author, resolved)
	default:
		return nil, []Issue{errorIssue(author.ID, author.Path+"/type", "renderer_not_implemented", fmt.Sprintf("display-list compile does not support node type %q", author.Type))}
	}
}

func drawOpsForRule(author AuthorNode, resolved ResolvedNode) ([]DrawOp, []Issue) {
	if author.Rule == nil {
		return nil, []Issue{errorIssue(author.ID, author.Path, "internal_compile_error", "rule author node is missing rule spec")}
	}
	rule := author.Rule
	from := Point{}
	to := Point{}
	if rule.Orientation == "vertical" {
		x := (resolved.Box.W - 1) / 2
		from = Point{X: x, Y: rule.InsetStart}
		to = Point{X: x, Y: resolved.Box.H - 1 - rule.InsetEnd}
	} else {
		y := (resolved.Box.H - 1) / 2
		from = Point{X: rule.InsetStart, Y: y}
		to = Point{X: resolved.Box.W - 1 - rule.InsetEnd, Y: y}
	}
	strokeWidth := rule.StrokeWidth
	return []DrawOp{{
		ID:        author.ID + ".__line",
		Role:      "rule.line",
		Source:    sourceRefForAuthorNode(author),
		Generated: true,
		Command: CoreCommand{Line: &CoreLineCommand{
			Box:         resolved.Box,
			From:        &from,
			To:          &to,
			Stroke:      rule.Stroke,
			StrokeWidth: &strokeWidth,
		}},
	}}, nil
}

func drawOpsForCard(author AuthorNode, resolved ResolvedNode) ([]DrawOp, []Issue) {
	if author.Card == nil {
		return nil, []Issue{errorIssue(author.ID, author.Path, "internal_compile_error", "card author node is missing card spec")}
	}
	card := author.Card
	ops := []DrawOp{}
	strokeWidth := card.StrokeWidth
	radius := card.Radius
	ops = append(ops, DrawOp{
		ID:        author.ID + ".__bg",
		Role:      "card.bg",
		Source:    sourceRefForAuthorNode(author),
		Generated: true,
		Command: CoreCommand{Rect: &CoreRectCommand{
			Box:         resolved.Box,
			Fill:        card.Fill,
			Stroke:      card.Stroke,
			StrokeWidth: &strokeWidth,
			Radius:      &radius,
		}},
	})
	if card.Header == nil && card.Body == nil {
		return ops, nil
	}
	content := Box{X: resolved.Box.X + card.Padding, Y: resolved.Box.Y + card.Padding, W: resolved.Box.W - 2*card.Padding, H: resolved.Box.H - 2*card.Padding}
	bodyBox := content
	if card.Header != nil {
		headerBox := Box{X: content.X, Y: content.Y, W: content.W, H: card.Header.Height}
		ops = append(ops, DrawOp{
			ID:        author.ID + ".__header_bg",
			Role:      "card.header_bg",
			Source:    sourceRefForCardHeader(author),
			Generated: true,
			Command: CoreCommand{Rect: &CoreRectCommand{
				Box:  headerBox,
				Fill: card.Header.Fill,
			}},
		})
		ops = append(ops, DrawOp{
			ID:        author.ID + ".__header_text",
			Role:      "card.header_text",
			Source:    sourceRefForCardHeader(author),
			Generated: true,
			Command:   CoreCommand{Text: textCommandFromCardText(card.Header, headerBox)},
		})
		bodyBox = Box{X: content.X, Y: content.Y + card.Header.Height + card.HeaderGap, W: content.W, H: content.H - card.Header.Height - card.HeaderGap}
	}
	if card.Body != nil {
		ops = append(ops, DrawOp{
			ID:        author.ID + ".__body_text",
			Role:      "card.body_text",
			Source:    sourceRefForCardBody(author),
			Generated: true,
			Command:   CoreCommand{Text: textCommandFromCardText(card.Body, bodyBox)},
		})
	}
	return ops, nil
}

func textCommandFromCardText(spec *CardTextSpec, box Box) *CoreTextCommand {
	wrap := spec.Wrap
	shrinkToFit := spec.ShrinkToFit
	return &CoreTextCommand{
		Box:           box,
		Text:          spec.Text,
		Font:          cloneFontPtr(&spec.Font),
		Align:         spec.Align,
		VAlign:        spec.VAlign,
		Color:         spec.Color,
		Wrap:          &wrap,
		MaxLines:      cloneIntPtr(spec.MaxLines),
		ShrinkToFit:   &shrinkToFit,
		ShrinkMinSize: cloneIntPtr(spec.ShrinkMinSize),
		LineHeight:    cloneFloatPtr(spec.LineHeight),
	}
}

func sourceRefForCardHeader(node AuthorNode) SourceRef {
	ref := sourceRefForAuthorNode(node)
	base := node.Path + "/header"
	ref.FieldPath = cardTextFieldPaths(base)
	ref.FieldPath["fill"] = base + "/fill"
	ref.FieldPath["height"] = base + "/height"
	ref.FieldPath["box"] = base
	return ref
}

func sourceRefForCardBody(node AuthorNode) SourceRef {
	ref := sourceRefForAuthorNode(node)
	base := node.Path + "/body"
	ref.FieldPath = cardTextFieldPaths(base)
	ref.FieldPath["box"] = base
	return ref
}

func cardTextFieldPaths(base string) map[string]string {
	return map[string]string{
		"text":            base + "/text",
		"font":            base + "/font",
		"font.size":       base + "/font/size",
		"font.family":     base + "/font/family",
		"font.weight":     base + "/font/weight",
		"color":           base + "/color",
		"align":           base + "/align",
		"valign":          base + "/valign",
		"wrap":            base + "/wrap",
		"max_lines":       base + "/max_lines",
		"shrink_to_fit":   base + "/shrink_to_fit",
		"shrink_min_size": base + "/shrink_min_size",
		"line_height":     base + "/line_height",
	}
}

func sourceRefForAuthorNode(node AuthorNode) SourceRef {
	return SourceRef{
		ID:        node.ID,
		Type:      node.Type,
		Path:      node.Path,
		FieldPath: fieldPathsForAuthorNode(node),
	}
}

func fieldPathsForAuthorNode(node AuthorNode) map[string]string {
	paths := map[string]string{
		"id":   node.Path + "/id",
		"type": node.Path + "/type",
		"box":  node.BoxPath,
	}
	switch NodeType(node.Type) {
	case NodeTypeText:
		paths["text"] = node.Path + "/text"
		paths["font"] = node.Path + "/font"
		paths["font.size"] = node.Path + "/font/size"
		paths["font.family"] = node.Path + "/font/family"
		paths["font.weight"] = node.Path + "/font/weight"
		paths["align"] = node.Path + "/align"
		paths["valign"] = node.Path + "/valign"
		paths["color"] = node.Path + "/color"
		paths["wrap"] = node.Path + "/wrap"
		paths["max_lines"] = node.Path + "/max_lines"
		paths["shrink_to_fit"] = node.Path + "/shrink_to_fit"
		paths["shrink_min_size"] = node.Path + "/shrink_min_size"
		paths["line_height"] = node.Path + "/line_height"
	case NodeTypeRect:
		paths["fill"] = node.Path + "/fill"
		paths["stroke"] = node.Path + "/stroke"
		paths["stroke_width"] = node.Path + "/stroke_width"
		paths["radius"] = node.Path + "/radius"
	case NodeTypeLine:
		paths["from"] = node.Path + "/from"
		paths["to"] = node.Path + "/to"
		paths["stroke"] = node.Path + "/stroke"
		paths["stroke_width"] = node.Path + "/stroke_width"
	case NodeTypeImage:
		paths["src"] = node.Path + "/src"
		paths["fit"] = node.Path + "/fit"
		paths["position"] = node.Path + "/position"
		paths["background"] = node.Path + "/background"
	case NodeTypeRule:
		paths["orientation"] = node.Path + "/orientation"
		paths["stroke"] = node.Path + "/stroke"
		paths["stroke_width"] = node.Path + "/stroke_width"
		paths["inset_start"] = node.Path + "/inset_start"
		paths["inset_end"] = node.Path + "/inset_end"
	case NodeTypeCard:
		paths["fill"] = node.Path + "/fill"
		paths["stroke"] = node.Path + "/stroke"
		paths["stroke_width"] = node.Path + "/stroke_width"
		paths["radius"] = node.Path + "/radius"
		paths["padding"] = node.Path + "/padding"
		paths["header_gap"] = node.Path + "/header_gap"
		paths["header"] = node.Path + "/header"
		paths["body"] = node.Path + "/body"
	case NodeTypeGrid:
		paths["columns"] = node.Path + "/columns"
		paths["rows"] = node.Path + "/rows"
		paths["gap_x"] = node.Path + "/gap_x"
		paths["gap_y"] = node.Path + "/gap_y"
		paths["children"] = node.Path + "/children"
	case NodeTypeList:
		paths["items"] = node.Path + "/items"
		paths["marker"] = node.Path + "/marker"
		paths["font"] = node.Path + "/font"
		paths["font.size"] = node.Path + "/font/size"
		paths["font.family"] = node.Path + "/font/family"
		paths["font.weight"] = node.Path + "/font/weight"
		paths["color"] = node.Path + "/color"
		paths["line_height"] = node.Path + "/line_height"
		paths["shrink_to_fit"] = node.Path + "/shrink_to_fit"
		paths["shrink_min_size"] = node.Path + "/shrink_min_size"
		paths["marker_gap"] = node.Path + "/marker_gap"
		paths["item_gap"] = node.Path + "/item_gap"
	case NodeTypeStack:
		paths["direction"] = node.Path + "/direction"
		paths["gap"] = node.Path + "/gap"
		paths["children"] = node.Path + "/children"
	case NodeTypeSection:
		paths["title"] = node.Path + "/title"
		paths["title_font"] = node.Path + "/title_font"
		paths["title_color"] = node.Path + "/title_color"
		paths["title_height"] = node.Path + "/title_height"
		paths["title_align"] = node.Path + "/title_align"
		paths["title_valign"] = node.Path + "/title_valign"
		paths["gap"] = node.Path + "/gap"
		paths["rule"] = node.Path + "/rule"
		paths["rule_stroke"] = node.Path + "/rule_stroke"
		paths["rule_stroke_width"] = node.Path + "/rule_stroke_width"
		paths["rule_height"] = node.Path + "/rule_height"
		paths["children"] = node.Path + "/children"
	}
	return paths
}

func validateDrawOpIDs(list DisplayList, authorIDs map[string]bool) []Issue {
	seen := map[string]bool{}
	var issues []Issue
	for _, op := range list.Ops {
		if op.ID == "" {
			issues = append(issues, errorIssue(op.Source.ID, op.Source.Path, "missing_required", "draw op id must not be empty"))
			continue
		}
		if seen[op.ID] {
			issues = append(issues, errorIssue(op.Source.ID, op.Source.Path, "draw_op_id_collision", fmt.Sprintf("draw op id %q duplicates another draw op", op.ID)))
			continue
		}
		seen[op.ID] = true
		if op.Generated && authorIDs[op.ID] {
			issues = append(issues, errorIssue(op.Source.ID, op.Source.Path, "draw_op_id_collision", fmt.Sprintf("generated draw op id %q collides with an author node id", op.ID)))
		}
	}
	return issues
}

func (c CoreCommand) typeName() string {
	switch {
	case c.Rect != nil:
		return string(NodeTypeRect)
	case c.Text != nil:
		return string(NodeTypeText)
	case c.Line != nil:
		return string(NodeTypeLine)
	case c.Image != nil:
		return string(NodeTypeImage)
	default:
		return ""
	}
}

func (c CoreCommand) box() Box {
	switch {
	case c.Rect != nil:
		return c.Rect.Box
	case c.Text != nil:
		return c.Text.Box
	case c.Line != nil:
		return c.Line.Box
	case c.Image != nil:
		return c.Image.Box
	default:
		return Box{}
	}
}

func (c CoreCommand) commandCount() int {
	n := 0
	if c.Rect != nil {
		n++
	}
	if c.Text != nil {
		n++
	}
	if c.Line != nil {
		n++
	}
	if c.Image != nil {
		n++
	}
	return n
}

func isLowLevelNodeType(typ string) bool {
	switch NodeType(typ) {
	case NodeTypeText, NodeTypeRect, NodeTypeLine, NodeTypeImage:
		return true
	default:
		return false
	}
}

func cloneFontPtr(v *Font) *Font {
	if v == nil {
		return nil
	}
	copy := *v
	return &copy
}

func cloneIntPtr(v *int) *int {
	if v == nil {
		return nil
	}
	copy := *v
	return &copy
}

func cloneBoolPtr(v *bool) *bool {
	if v == nil {
		return nil
	}
	copy := *v
	return &copy
}

func cloneFloatPtr(v *float64) *float64 {
	if v == nil {
		return nil
	}
	copy := *v
	return &copy
}

func clonePointPtr(v *Point) *Point {
	if v == nil {
		return nil
	}
	copy := *v
	return &copy
}
