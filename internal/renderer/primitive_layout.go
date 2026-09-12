package renderer

import (
	"fmt"

	xfont "golang.org/x/image/font"
)

type listItemLayoutInternal struct {
	Index     int
	Marker    string
	MarkerBox *Box
	TextBox   Box
	Lines     int
}

func layoutList(author AuthorNode, box Box) (*ListLayoutReport, []listItemLayoutInternal, []Issue) {
	if author.List == nil {
		return nil, nil, []Issue{errorIssue(author.ID, author.Path, "internal_compile_error", "list author node is missing list spec")}
	}
	list := author.List
	font := list.Font
	if font.Family == "" {
		font.Family = FontFamilySans
	}
	if font.Weight == "" {
		font.Weight = FontWeightRegular
	}
	lineHeight := DefaultLineHeight
	if list.LineHeight != nil {
		lineHeight = *list.LineHeight
	}
	minSize := font.Size
	if list.ShrinkToFit {
		minSize = list.ShrinkMinSize
		if minSize <= 0 {
			minSize = DefaultShrinkMinSize
		}
		if minSize > font.Size {
			minSize = font.Size
		}
	}

	var lastReport *ListLayoutReport
	var lastItems []listItemLayoutInternal
	var lastReason string
	for size := font.Size; size >= minSize; size-- {
		report, items, fits, reason, issues := layoutListAtFont(author, box, size, lineHeight)
		if len(issues) > 0 {
			return report, items, issues
		}
		lastReport = report
		lastItems = items
		lastReason = reason
		if fits {
			return report, items, nil
		}
		if !list.ShrinkToFit {
			break
		}
	}
	if lastReport == nil {
		lastReport = &ListLayoutReport{Marker: list.Marker, ItemCount: len(list.Items), FontSize: font.Size, MarkerGap: list.MarkerGap, ItemGap: list.ItemGap}
	}
	message := lastReason
	if message == "" {
		message = "list items do not fit inside node box"
	}
	if list.ShrinkToFit {
		message = fmt.Sprintf("%s; list still does not fit at shrink_min_size=%d", message, minSize)
	}
	return lastReport, lastItems, []Issue{errorIssue(author.ID, author.Path+"/items", "list_overflow", message)}
}

func layoutListAtFont(author AuthorNode, box Box, fontSize int, lineHeight float64) (*ListLayoutReport, []listItemLayoutInternal, bool, string, []Issue) {
	list := author.List
	font := list.Font
	font.Size = fontSize
	font = normalizedFont(font)
	face, err := newFace(font.Family, font.Weight, font.Size)
	if err != nil {
		return nil, nil, false, "", []Issue{errorIssue(author.ID, author.Path+"/font", "font_load_failed", err.Error())}
	}
	defer face.Close()

	markerTexts := make([]string, len(list.Items))
	maxMarkerWidth := 0
	for i := range list.Items {
		marker := listMarkerText(list.Marker, i)
		markerTexts[i] = marker
		width := listMarkerWidth(face, fontSize, list.Marker, marker)
		if width > maxMarkerWidth {
			maxMarkerWidth = width
		}
	}
	markerColumnWidth := 0
	if maxMarkerWidth > 0 {
		markerColumnWidth = maxMarkerWidth + list.MarkerGap
	}
	itemWidth := box.W - markerColumnWidth
	report := &ListLayoutReport{Marker: list.Marker, ItemCount: len(list.Items), FontSize: fontSize, MarkerColumnWidth: markerColumnWidth, MarkerGap: list.MarkerGap, ItemGap: list.ItemGap}
	if itemWidth <= 0 {
		return report, nil, false, "list marker column leaves no positive item text width", nil
	}

	items := make([]listItemLayoutInternal, 0, len(list.Items))
	y := box.Y
	totalHeight := 0
	for i, item := range list.Items {
		layout := layoutText(face, item, Box{W: itemWidth, H: 1 << 30}, fontSize, lineHeight, true, nil)
		if !layout.fits {
			reason := layout.reason
			if reason == "" {
				reason = fmt.Sprintf("list item %d does not fit", i)
			}
			return report, items, false, reason, nil
		}
		itemHeight := layout.totalHeight
		if itemHeight <= 0 {
			itemHeight = fontSize
		}
		markerBox := (*Box)(nil)
		if markerTexts[i] != "" {
			mb := Box{X: box.X, Y: y, W: maxMarkerWidth, H: itemHeight}
			markerBox = &mb
		}
		textBox := Box{X: box.X + markerColumnWidth, Y: y, W: itemWidth, H: itemHeight}
		itemLayout := listItemLayoutInternal{Index: i, Marker: markerTexts[i], MarkerBox: markerBox, TextBox: textBox, Lines: len(layout.lines)}
		items = append(items, itemLayout)
		report.Items = append(report.Items, ListItemLayoutReport{Index: i, Marker: markerTexts[i], MarkerBox: cloneBoxPtr(markerBox), TextBox: textBox, Lines: len(layout.lines)})
		totalHeight += itemHeight
		y += itemHeight
		if i != len(list.Items)-1 {
			totalHeight += list.ItemGap
			y += list.ItemGap
		}
	}
	if totalHeight > box.H {
		return report, items, false, fmt.Sprintf("list height %d exceeds box height %d", totalHeight, box.H), nil
	}
	return report, items, true, "", nil
}

func listMarkerText(marker string, index int) string {
	switch marker {
	case "bullet":
		return "•"
	case "dash":
		return "-"
	case "number":
		return fmt.Sprintf("%d.", index+1)
	case "check":
		return "check"
	case "none":
		return ""
	default:
		return "•"
	}
}

func listMarkerWidth(face xfont.Face, fontSize int, marker, markerText string) int {
	if markerText == "" {
		return 0
	}
	if marker == "check" {
		return maxInt(1, fontSize)
	}
	return measuredLine(face, markerText).widthPx
}

func drawOpsForList(author AuthorNode, resolved ResolvedNode) ([]DrawOp, []Issue) {
	layout, items, issues := layoutList(author, resolved.Box)
	if len(issues) > 0 {
		return nil, issues
	}
	if layout == nil || author.List == nil {
		return nil, nil
	}
	list := author.List
	font := list.Font
	font.Size = layout.FontSize
	wrap := true
	shrink := false
	lineHeight := list.LineHeight
	ops := make([]DrawOp, 0, len(items)*3)
	for _, item := range items {
		textPath := fmt.Sprintf("%s/items/%d", author.Path, item.Index)
		if item.MarkerBox != nil {
			markerSource := sourceRefForListMarker(author, item.Index)
			if list.Marker == "check" {
				ops = append(ops, drawOpsForCheckMarker(author, item.Index, *item.MarkerBox, list.Color, layout.FontSize, markerSource)...)
			} else {
				ops = append(ops, DrawOp{
					ID:        fmt.Sprintf("%s.__marker_%d", author.ID, item.Index),
					Role:      fmt.Sprintf("list.marker.%d", item.Index),
					Source:    markerSource,
					Generated: true,
					Command: CoreCommand{Text: &CoreTextCommand{
						Box:           *item.MarkerBox,
						Text:          item.Marker,
						Font:          cloneFontPtr(&font),
						Align:         AlignRight,
						VAlign:        VAlignTop,
						Color:         list.Color,
						Wrap:          &wrap,
						ShrinkToFit:   &shrink,
						ShrinkMinSize: &list.ShrinkMinSize,
						LineHeight:    cloneFloatPtr(lineHeight),
					}},
				})
			}
		}
		itemSource := sourceRefForListItem(author, item.Index, textPath)
		ops = append(ops, DrawOp{
			ID:        fmt.Sprintf("%s.__item_%d", author.ID, item.Index),
			Role:      fmt.Sprintf("list.item.%d", item.Index),
			Source:    itemSource,
			Generated: true,
			Command: CoreCommand{Text: &CoreTextCommand{
				Box:           item.TextBox,
				Text:          list.Items[item.Index],
				Font:          cloneFontPtr(&font),
				Align:         AlignLeft,
				VAlign:        VAlignTop,
				Color:         list.Color,
				Wrap:          &wrap,
				ShrinkToFit:   &shrink,
				ShrinkMinSize: &list.ShrinkMinSize,
				LineHeight:    cloneFloatPtr(lineHeight),
			}},
		})
	}
	return ops, nil
}

func drawOpsForCheckMarker(author AuthorNode, index int, box Box, color string, fontSize int, source SourceRef) []DrawOp {
	strokeWidth := maxInt(1, minInt(fontSize/8, minInt(box.W, box.H)/4))
	margin := strokeWidth + 1
	if margin*2 >= box.W || margin*2 >= box.H {
		strokeWidth = 1
		margin = 1
	}
	fromA := Point{X: clampInt(margin, 0, box.W-1), Y: clampInt((box.H*58)/100, 0, box.H-1)}
	join := Point{X: clampInt((box.W*40)/100, 0, box.W-1), Y: clampInt(box.H-margin-1, 0, box.H-1)}
	toB := Point{X: clampInt(box.W-margin-1, 0, box.W-1), Y: clampInt(margin, 0, box.H-1)}
	return []DrawOp{
		{
			ID:        fmt.Sprintf("%s.__marker_%d_a", author.ID, index),
			Role:      fmt.Sprintf("list.marker.%d.a", index),
			Source:    source,
			Generated: true,
			Command:   CoreCommand{Line: &CoreLineCommand{Box: box, From: &fromA, To: &join, Stroke: color, StrokeWidth: &strokeWidth}},
		},
		{
			ID:        fmt.Sprintf("%s.__marker_%d_b", author.ID, index),
			Role:      fmt.Sprintf("list.marker.%d.b", index),
			Source:    source,
			Generated: true,
			Command:   CoreCommand{Line: &CoreLineCommand{Box: box, From: &join, To: &toB, Stroke: color, StrokeWidth: &strokeWidth}},
		},
	}
}

func sourceRefForListItem(node AuthorNode, index int, textPath string) SourceRef {
	ref := sourceRefForAuthorNode(node)
	ref.FieldPath = fieldPathsForAuthorNode(node)
	ref.FieldPath["text"] = textPath
	ref.FieldPath["box"] = textPath
	return ref
}

func sourceRefForListMarker(node AuthorNode, index int) SourceRef {
	ref := sourceRefForAuthorNode(node)
	ref.FieldPath = fieldPathsForAuthorNode(node)
	ref.FieldPath["text"] = fmt.Sprintf("%s/marker", node.Path)
	ref.FieldPath["box"] = fmt.Sprintf("%s/items/%d", node.Path, index)
	return ref
}

func resolveStackChildren(author AuthorNode, box Box, resolved *ResolvedScene) ([]string, *StackLayoutReport, []Issue) {
	if author.Stack == nil {
		return nil, nil, []Issue{errorIssue(author.ID, author.Path, "internal_compile_error", "stack author node is missing stack spec")}
	}
	return resolveStackChildSpecs(author.ID, author.Path, author.Stack.Direction, author.Stack.Gap, author.Stack.Children, box, resolved)
}

func resolveStackChildSpecs(parentID, parentPath, direction string, gap int, children []StackChildSpec, box Box, resolved *ResolvedScene) ([]string, *StackLayoutReport, []Issue) {
	layout := &StackLayoutReport{Direction: direction, Gap: gap}
	var issues []Issue
	if len(children) == 0 {
		return nil, layout, issues
	}
	total := gap * (len(children) - 1)
	for _, child := range children {
		total += child.Size
	}
	if direction == "horizontal" {
		if total > box.W {
			return nil, layout, []Issue{errorIssue(parentID, parentPath+"/children", "stack_overflow", fmt.Sprintf("stack child widths plus gaps total %d exceeds box width %d", total, box.W))}
		}
	} else if total > box.H {
		return nil, layout, []Issue{errorIssue(parentID, parentPath+"/children", "stack_overflow", fmt.Sprintf("stack child heights plus gaps total %d exceeds box height %d", total, box.H))}
	}

	childIDs := make([]string, 0, len(children))
	cursor := 0
	for _, child := range children {
		childBox := Box{}
		if direction == "horizontal" {
			childBox = Box{X: box.X + cursor, Y: box.Y, W: child.Size, H: box.H}
		} else {
			childBox = Box{X: box.X, Y: box.Y + cursor, W: box.W, H: child.Size}
		}
		childIDs = append(childIDs, child.Node.ID)
		layout.Children = append(layout.Children, StackChildLayoutReport{ID: child.Node.ID, Size: child.Size, Box: childBox})
		issues = append(issues, resolveAuthorNode(child.Node, childBox, BoxSourceAssigned, parentID, resolved)...)
		cursor += child.Size + gap
	}
	return childIDs, layout, issues
}

func resolveSection(author AuthorNode, box Box, resolved *ResolvedScene) ([]string, *SectionLayoutReport, []Issue) {
	if author.Section == nil {
		return nil, nil, []Issue{errorIssue(author.ID, author.Path, "internal_compile_error", "section author node is missing section spec")}
	}
	section := author.Section
	layout := &SectionLayoutReport{Gap: section.Gap}
	var issues []Issue
	if section.TitleHeight <= 0 || section.TitleHeight > box.H {
		return nil, layout, []Issue{errorIssue(author.ID, author.Path+"/title_height", "out_of_range", "section title_height must fit inside the section box")}
	}
	titleBox := Box{X: box.X, Y: box.Y, W: box.W, H: section.TitleHeight}
	layout.TitleBox = &titleBox
	cursor := section.TitleHeight
	if section.Rule {
		if section.RuleHeight < section.RuleStrokeWidth {
			return nil, layout, []Issue{errorIssue(author.ID, author.Path+"/rule_height", "out_of_range", "section rule_height must be at least rule_stroke_width")}
		}
		cursor += section.Gap
		if cursor+section.RuleHeight > box.H {
			return nil, layout, []Issue{errorIssue(author.ID, author.Path+"/rule_height", "out_of_range", "section rule must fit inside the section box")}
		}
		ruleBox := Box{X: box.X, Y: box.Y + cursor, W: box.W, H: section.RuleHeight}
		layout.RuleBox = &ruleBox
		cursor += section.RuleHeight
	}
	if len(section.Children) == 0 {
		return nil, layout, issues
	}
	cursor += section.Gap
	contentBox := Box{X: box.X, Y: box.Y + cursor, W: box.W, H: box.H - cursor}
	layout.ContentBox = &contentBox
	if contentBox.H <= 0 {
		return nil, layout, []Issue{errorIssue(author.ID, author.Path+"/children", "section_overflow", sectionOverflowMessage(0, contentBox.H, cursor, section))}
	}
	childrenHeight := stackChildrenTotalSize(section.Gap, section.Children)
	if childrenHeight > contentBox.H {
		return nil, layout, []Issue{errorIssue(author.ID, author.Path+"/children", "section_overflow", sectionOverflowMessage(childrenHeight, contentBox.H, cursor, section))}
	}
	childIDs, stackLayout, childIssues := resolveStackChildSpecs(author.ID, author.Path, "vertical", section.Gap, section.Children, contentBox, resolved)
	issues = append(issues, childIssues...)
	if stackLayout != nil {
		layout.Children = stackLayout.Children
	}
	return childIDs, layout, issues
}

func stackChildrenTotalSize(gap int, children []StackChildSpec) int {
	if len(children) == 0 {
		return 0
	}
	total := gap * (len(children) - 1)
	for _, child := range children {
		total += child.Size
	}
	return total
}

func sectionOverflowMessage(childrenHeight, availableHeight, reservedHeight int, section *SectionSpec) string {
	if availableHeight < 0 {
		availableHeight = 0
	}
	ruleText := "disabled"
	if section.Rule {
		ruleText = fmt.Sprintf("enabled, height=%d", section.RuleHeight)
	}
	if childrenHeight <= 0 {
		return fmt.Sprintf("section leaves no positive content area for children after reserving %dpx for title/rule/gaps (title_height=%d, rule=%s, gap=%d)", reservedHeight, section.TitleHeight, ruleText, section.Gap)
	}
	return fmt.Sprintf("section children need %dpx but only %dpx is available after reserving %dpx for title/rule/gaps (title_height=%d, rule=%s, gap=%d)", childrenHeight, availableHeight, reservedHeight, section.TitleHeight, ruleText, section.Gap)
}

func drawOpsForSection(author AuthorNode, resolved ResolvedNode) ([]DrawOp, []Issue) {
	if author.Section == nil {
		return nil, []Issue{errorIssue(author.ID, author.Path, "internal_compile_error", "section author node is missing section spec")}
	}
	section := author.Section
	_, layout, issues := resolveSectionForDraw(author, resolved.Box)
	if len(issues) > 0 {
		return nil, issues
	}
	if layout == nil || layout.TitleBox == nil {
		return nil, nil
	}
	wrap := false
	shrink := false
	ops := []DrawOp{{
		ID:        author.ID + ".__title_text",
		Role:      "section.title_text",
		Source:    sourceRefForSectionTitle(author),
		Generated: true,
		Command: CoreCommand{Text: &CoreTextCommand{
			Box:           *layout.TitleBox,
			Text:          section.Title,
			Font:          cloneFontPtr(&section.TitleFont),
			Align:         section.TitleAlign,
			VAlign:        section.TitleVAlign,
			Color:         section.TitleColor,
			Wrap:          &wrap,
			ShrinkToFit:   &shrink,
			ShrinkMinSize: intPtr(DefaultShrinkMinSize),
		}},
	}}
	if section.Rule && layout.RuleBox != nil {
		strokeWidth := section.RuleStrokeWidth
		y := (layout.RuleBox.H - 1) / 2
		from := Point{X: 0, Y: y}
		to := Point{X: layout.RuleBox.W - 1, Y: y}
		ops = append(ops, DrawOp{
			ID:        author.ID + ".__rule_line",
			Role:      "section.rule_line",
			Source:    sourceRefForSectionRule(author),
			Generated: true,
			Command:   CoreCommand{Line: &CoreLineCommand{Box: *layout.RuleBox, From: &from, To: &to, Stroke: section.RuleStroke, StrokeWidth: &strokeWidth}},
		})
	}
	return ops, nil
}

func resolveSectionForDraw(author AuthorNode, box Box) ([]string, *SectionLayoutReport, []Issue) {
	resolved := &ResolvedScene{}
	return resolveSection(author, box, resolved)
}

func sourceRefForSectionTitle(node AuthorNode) SourceRef {
	ref := sourceRefForAuthorNode(node)
	ref.FieldPath = fieldPathsForAuthorNode(node)
	ref.FieldPath["text"] = node.Path + "/title"
	ref.FieldPath["font"] = node.Path + "/title_font"
	ref.FieldPath["font.size"] = node.Path + "/title_font/size"
	ref.FieldPath["font.family"] = node.Path + "/title_font/family"
	ref.FieldPath["font.weight"] = node.Path + "/title_font/weight"
	ref.FieldPath["color"] = node.Path + "/title_color"
	ref.FieldPath["align"] = node.Path + "/title_align"
	ref.FieldPath["valign"] = node.Path + "/title_valign"
	return ref
}

func sourceRefForSectionRule(node AuthorNode) SourceRef {
	ref := sourceRefForAuthorNode(node)
	ref.FieldPath = fieldPathsForAuthorNode(node)
	ref.FieldPath["stroke"] = node.Path + "/rule_stroke"
	ref.FieldPath["stroke_width"] = node.Path + "/rule_stroke_width"
	ref.FieldPath["box"] = node.Path + "/rule_height"
	return ref
}

func cloneBoxPtr(v *Box) *Box {
	if v == nil {
		return nil
	}
	copy := *v
	return &copy
}
