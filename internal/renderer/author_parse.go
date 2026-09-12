package renderer

import (
	"encoding/json"
	"fmt"
)

type authorNodeContext string

const (
	authorNodeTopLevel authorNodeContext = "top-level"
	authorNodeAssigned authorNodeContext = "assigned"
)

func parseAuthorSceneStrict(data []byte) (AuthorScene, []Issue) {
	scene := AuthorScene{Canvas: Canvas{Width: CanvasWidth, Height: CanvasHeight, Background: DefaultCanvasBackground}}
	var issues []Issue
	if issue := checkSceneLimits(data); issue != nil {
		return scene, []Issue{*issue}
	}

	root, ok := decodeObjectRaw(data, "", "scene", "root scene must be a JSON object", &issues)
	if !ok {
		return scene, issues
	}
	if hasTrailingJSONValue(data) {
		issues = append(issues, errorIssue("", "", "invalid_json", "scene contains trailing JSON after the root object"))
		return scene, issues
	}

	addUnknownFields(root, "", allowedSet("$schema", "version", "canvas", "nodes"), "", &issues)
	if raw, ok := root["$schema"]; ok {
		scene.Schema, _ = decodeStringRaw(raw, "/$schema", "", &issues)
	}
	if raw, ok := root["version"]; ok {
		if value, fieldOK := decodeStringRaw(raw, "/version", "", &issues); fieldOK {
			scene.Version = value
			if value == "" {
				issues = append(issues, errorIssue("", "/version", "bad_enum", fmt.Sprintf("version must be %q", SceneVersionV1)))
			}
		}
	} else {
		issues = append(issues, errorIssue("", "/version", "missing_required", "version is required"))
	}
	if raw, ok := root["canvas"]; ok {
		scene.Canvas = parseCanvas(raw, &issues)
	}
	if raw, ok := root["nodes"]; ok {
		scene.Nodes = parseAuthorNodes(raw, "/nodes", authorNodeTopLevel, scene.Canvas.Background, &issues)
	} else {
		issues = append(issues, errorIssue("", "/nodes", "missing_required", "nodes is required"))
	}
	issues = append(issues, validateAuthorScene(scene)...)
	return scene, issues
}

func parseAuthorNodes(raw json.RawMessage, path string, ctx authorNodeContext, canvasBackground string, issues *[]Issue) []AuthorNode {
	if !rawStartsWith(raw, '[') {
		*issues = append(*issues, errorIssue("", path, "invalid_type", pathLeafName(path)+" must be an array"))
		return nil
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(raw, &raws); err != nil {
		*issues = append(*issues, errorIssue("", path, "invalid_type", pathLeafName(path)+" must be an array"))
		return nil
	}
	nodes := make([]AuthorNode, 0, len(raws))
	for i, nodeRaw := range raws {
		nodes = append(nodes, parseAuthorNode(nodeRaw, fmt.Sprintf("%s/%d", path, i), ctx, canvasBackground, issues))
	}
	return nodes
}

func parseAuthorNode(raw json.RawMessage, path string, ctx authorNodeContext, canvasBackground string, issues *[]Issue) AuthorNode {
	node := AuthorNode{Path: path}
	m, ok := decodeObjectMessage(raw, path, "", "node must be an object", issues)
	if !ok {
		return node
	}

	nodeID := ""
	if raw, ok := m["id"]; ok {
		if value, fieldOK := decodeStringRaw(raw, path+"/id", "", issues); fieldOK {
			nodeID = value
			node.ID = value
			if value == "" {
				*issues = append(*issues, errorIssue(nodeID, path+"/id", "missing_required", "node id must not be empty"))
			}
		}
	}
	typeDecoded := false
	if raw, ok := m["type"]; ok {
		if typeString, fieldOK := decodeStringRaw(raw, path+"/type", nodeID, issues); fieldOK {
			typeDecoded = true
			node.Type = typeString
		}
	}

	allowed := allowedAuthorNodeFields(NodeType(node.Type))
	if allowed == nil {
		allowed = allKnownAuthorNodeFields()
	}
	addUnknownFields(m, path, allowed, nodeID, issues)

	if _, ok := m["id"]; !ok {
		*issues = append(*issues, errorIssue(nodeID, path+"/id", "missing_required", "node id is required"))
	}
	if _, ok := m["type"]; !ok {
		*issues = append(*issues, errorIssue(nodeID, path+"/type", "missing_required", "node type is required"))
	}
	if raw, ok := m["box"]; ok {
		if ctx == authorNodeAssigned {
			*issues = append(*issues, errorIssue(nodeID, path+"/box", "box_not_allowed", "child node box must be omitted because the parent layout assigns it"))
		} else {
			box := parseBox(raw, path+"/box", nodeID, issues)
			node.Box = &box
			node.BoxPath = path + "/box"
		}
	} else if ctx == authorNodeTopLevel {
		*issues = append(*issues, errorIssue(nodeID, path+"/box", "missing_required", "node box is required"))
	}

	switch NodeType(node.Type) {
	case NodeTypeText:
		parsed := Node{ID: node.ID, Type: NodeTypeText}
		if node.Box != nil {
			parsed.Box = *node.Box
		}
		parseTextNode(m, path, nodeID, &parsed, issues)
		node.Text = &TextSpec{Text: parsed.Text, Font: cloneFontPtr(parsed.Font), Align: parsed.Align, VAlign: parsed.VAlign, Color: parsed.Color, Wrap: cloneBoolPtr(parsed.Wrap), MaxLines: cloneIntPtr(parsed.MaxLines), ShrinkToFit: cloneBoolPtr(parsed.ShrinkToFit), ShrinkMinSize: cloneIntPtr(parsed.ShrinkMinSize), LineHeight: cloneFloatPtr(parsed.LineHeight)}
	case NodeTypeRect:
		parsed := Node{ID: node.ID, Type: NodeTypeRect}
		if node.Box != nil {
			parsed.Box = *node.Box
		}
		parseRectNode(m, path, nodeID, &parsed, issues)
		node.Rect = &RectSpec{Fill: parsed.Fill, Stroke: parsed.Stroke, StrokeWidth: cloneIntPtr(parsed.StrokeWidth), Radius: cloneIntPtr(parsed.Radius)}
	case NodeTypeLine:
		parsed := Node{ID: node.ID, Type: NodeTypeLine}
		if node.Box != nil {
			parsed.Box = *node.Box
		}
		parseLineNode(m, path, nodeID, &parsed, issues)
		node.Line = &LineSpec{From: clonePointPtr(parsed.From), To: clonePointPtr(parsed.To), Stroke: parsed.Stroke, StrokeWidth: cloneIntPtr(parsed.StrokeWidth)}
	case NodeTypeImage:
		node.Image = parseAuthorImageNode(m, path, nodeID, canvasBackground, issues)
	case NodeTypeRule:
		node.Rule = parseRuleNode(m, path, nodeID, issues)
	case NodeTypeCard:
		node.Card = parseCardNode(m, path, nodeID, issues)
	case NodeTypeGrid:
		node.Grid = parseGridNode(m, path, nodeID, canvasBackground, issues)
	case NodeTypeList:
		node.List = parseListNode(m, path, nodeID, issues)
	case NodeTypeStack:
		node.Stack = parseStackNode(m, path, nodeID, canvasBackground, issues)
	case NodeTypeSection:
		node.Section = parseSectionNode(m, path, nodeID, canvasBackground, issues)
	case "":
		if typeDecoded {
			*issues = append(*issues, errorIssue(nodeID, path+"/type", "bad_enum", "node type must be text, rect, line, image, rule, card, grid, list, stack, or section"))
		}
	default:
		*issues = append(*issues, errorIssue(nodeID, path+"/type", "bad_enum", fmt.Sprintf("unsupported node type %q", node.Type)))
	}
	return node
}

func parseAuthorImageNode(m map[string]json.RawMessage, path, nodeID, canvasBackground string, issues *[]Issue) *ImageSpec {
	image := &ImageSpec{Fit: ImageFitExact, Position: ImagePosition{X: 0.5, Y: 0.5}, Background: canvasBackground}
	if raw, ok := m["src"]; ok {
		src, fieldOK := decodeStringRaw(raw, path+"/src", nodeID, issues)
		image.Src = src
		if fieldOK {
			validateImageSrc(src, path+"/src", nodeID, issues)
		}
	} else {
		*issues = append(*issues, errorIssue(nodeID, path+"/src", "missing_required", "src is required for image nodes"))
	}
	if raw, ok := m["fit"]; ok {
		value, fieldOK := decodeStringRaw(raw, path+"/fit", nodeID, issues)
		if fieldOK && !isAllowedString(value, string(ImageFitExact), string(ImageFitContain), string(ImageFitCover), string(ImageFitStretch)) {
			*issues = append(*issues, errorIssue(nodeID, path+"/fit", "bad_enum", "fit must be exact, contain, cover, or stretch"))
		} else if fieldOK {
			image.Fit = ImageFit(value)
		}
	}
	if raw, ok := m["position"]; ok {
		pos, fieldOK := parseImagePosition(raw, path+"/position", nodeID, issues)
		if fieldOK {
			image.Position = pos
		}
	}
	if raw, ok := m["background"]; ok {
		if value, fieldOK := decodeColorField(raw, path+"/background", nodeID, issues); fieldOK {
			image.Background = value
		}
	}
	return image
}

func parseImagePosition(raw json.RawMessage, path, nodeID string, issues *[]Issue) (ImagePosition, bool) {
	if rawStartsWith(raw, '{') {
		m, ok := decodeObjectMessage(raw, path, nodeID, "position must be a string or object", issues)
		if !ok {
			return ImagePosition{}, false
		}
		addUnknownFields(m, path, allowedSet("x", "y"), nodeID, issues)
		pos := ImagePosition{}
		xOK, yOK := false, false
		if raw, ok := m["x"]; ok {
			pos.X, xOK = decodeFloatRaw(raw, path+"/x", nodeID, issues)
		} else {
			*issues = append(*issues, errorIssue(nodeID, path+"/x", "missing_required", "position.x is required"))
		}
		if raw, ok := m["y"]; ok {
			pos.Y, yOK = decodeFloatRaw(raw, path+"/y", nodeID, issues)
		} else {
			*issues = append(*issues, errorIssue(nodeID, path+"/y", "missing_required", "position.y is required"))
		}
		if xOK && (pos.X < 0 || pos.X > 1) {
			*issues = append(*issues, errorIssue(nodeID, path+"/x", "out_of_range", "position.x must be between 0 and 1"))
			xOK = false
		}
		if yOK && (pos.Y < 0 || pos.Y > 1) {
			*issues = append(*issues, errorIssue(nodeID, path+"/y", "out_of_range", "position.y must be between 0 and 1"))
			yOK = false
		}
		return pos, xOK && yOK
	}
	value, ok := decodeStringRaw(raw, path, nodeID, issues)
	if !ok {
		return ImagePosition{}, false
	}
	switch value {
	case "center":
		return ImagePosition{X: 0.5, Y: 0.5}, true
	case "top":
		return ImagePosition{X: 0.5, Y: 0}, true
	case "bottom":
		return ImagePosition{X: 0.5, Y: 1}, true
	case "left":
		return ImagePosition{X: 0, Y: 0.5}, true
	case "right":
		return ImagePosition{X: 1, Y: 0.5}, true
	default:
		*issues = append(*issues, errorIssue(nodeID, path, "bad_enum", "position must be center, top, bottom, left, right, or {x,y}"))
		return ImagePosition{}, false
	}
}

func parseRuleNode(m map[string]json.RawMessage, path, nodeID string, issues *[]Issue) *RuleSpec {
	rule := &RuleSpec{Orientation: "horizontal", Stroke: DefaultTextColor, StrokeWidth: 1}
	if raw, ok := m["orientation"]; ok {
		value, fieldOK := decodeStringRaw(raw, path+"/orientation", nodeID, issues)
		if fieldOK && !isAllowedString(value, "horizontal", "vertical") {
			*issues = append(*issues, errorIssue(nodeID, path+"/orientation", "bad_enum", "orientation must be horizontal or vertical"))
		} else if fieldOK {
			rule.Orientation = value
		}
	}
	if raw, ok := m["stroke"]; ok {
		if value, fieldOK := decodeColorField(raw, path+"/stroke", nodeID, issues); fieldOK {
			rule.Stroke = value
		}
	}
	if raw, ok := m["stroke_width"]; ok {
		if value, fieldOK := decodeIntRaw(raw, path+"/stroke_width", nodeID, issues); fieldOK {
			rule.StrokeWidth = value
			if value <= 0 {
				*issues = append(*issues, errorIssue(nodeID, path+"/stroke_width", "out_of_range", "stroke_width must be positive"))
			}
		}
	}
	if raw, ok := m["inset_start"]; ok {
		if value, fieldOK := decodeIntRaw(raw, path+"/inset_start", nodeID, issues); fieldOK {
			rule.InsetStart = value
			if value < 0 {
				*issues = append(*issues, errorIssue(nodeID, path+"/inset_start", "out_of_range", "inset_start must be non-negative"))
			}
		}
	}
	if raw, ok := m["inset_end"]; ok {
		if value, fieldOK := decodeIntRaw(raw, path+"/inset_end", nodeID, issues); fieldOK {
			rule.InsetEnd = value
			if value < 0 {
				*issues = append(*issues, errorIssue(nodeID, path+"/inset_end", "out_of_range", "inset_end must be non-negative"))
			}
		}
	}
	return rule
}

func parseCardNode(m map[string]json.RawMessage, path, nodeID string, issues *[]Issue) *CardSpec {
	card := &CardSpec{Fill: "#f4f4f4", Stroke: "#111111", StrokeWidth: 2, Radius: 16, Padding: 20, HeaderGap: 12}
	if raw, ok := m["fill"]; ok {
		if value, fieldOK := decodeColorField(raw, path+"/fill", nodeID, issues); fieldOK {
			card.Fill = value
		}
	}
	if raw, ok := m["stroke"]; ok {
		if value, fieldOK := decodeColorField(raw, path+"/stroke", nodeID, issues); fieldOK {
			card.Stroke = value
		}
	}
	parseNonNegativeIntField(m, "stroke_width", path, nodeID, &card.StrokeWidth, issues)
	parseNonNegativeIntField(m, "radius", path, nodeID, &card.Radius, issues)
	parseNonNegativeIntField(m, "padding", path, nodeID, &card.Padding, issues)
	parseNonNegativeIntField(m, "header_gap", path, nodeID, &card.HeaderGap, issues)
	if raw, ok := m["header"]; ok {
		card.Header = parseCardText(raw, path+"/header", nodeID, true, issues)
	}
	if raw, ok := m["body"]; ok {
		card.Body = parseCardText(raw, path+"/body", nodeID, false, issues)
	}
	return card
}

func parseCardText(raw json.RawMessage, path, nodeID string, header bool, issues *[]Issue) *CardTextSpec {
	spec := &CardTextSpec{}
	allowed := allowedSet("text", "font", "color", "align", "valign", "wrap", "max_lines", "shrink_to_fit", "shrink_min_size", "line_height")
	if header {
		allowed["height"] = true
		allowed["fill"] = true
		spec.Height = 56
		spec.Fill = "#dddddd"
		spec.Font = Font{Family: FontFamilySans, Size: 32, Weight: FontWeightBold}
		spec.Color = "#111111"
		spec.Align = AlignCenter
		spec.VAlign = VAlignMiddle
	} else {
		spec.Font = Font{Family: FontFamilySans, Size: 26, Weight: FontWeightRegular}
		spec.Color = "#111111"
		spec.Align = AlignLeft
		spec.VAlign = VAlignTop
		spec.Wrap = true
	}
	m, ok := decodeObjectMessage(raw, path, nodeID, pathLeafName(path)+" must be an object", issues)
	if !ok {
		return spec
	}
	addUnknownFields(m, path, allowed, nodeID, issues)
	if raw, ok := m["text"]; ok {
		spec.Text, _ = decodeStringRaw(raw, path+"/text", nodeID, issues)
	} else {
		*issues = append(*issues, errorIssue(nodeID, path+"/text", "missing_required", pathLeafName(path)+".text is required"))
	}
	if header {
		if raw, ok := m["height"]; ok {
			if value, fieldOK := decodeIntRaw(raw, path+"/height", nodeID, issues); fieldOK {
				spec.Height = value
				if value <= 0 {
					*issues = append(*issues, errorIssue(nodeID, path+"/height", "out_of_range", "height must be positive"))
				}
			}
		}
		if raw, ok := m["fill"]; ok {
			if value, fieldOK := decodeColorField(raw, path+"/fill", nodeID, issues); fieldOK {
				spec.Fill = value
			}
		}
	}
	parseCardTextCommon(m, path, nodeID, spec, issues)
	return spec
}

func parseCardTextCommon(m map[string]json.RawMessage, path, nodeID string, spec *CardTextSpec, issues *[]Issue) {
	if raw, ok := m["font"]; ok {
		spec.Font = parseFont(raw, path+"/font", nodeID, issues)
	}
	if raw, ok := m["color"]; ok {
		if value, fieldOK := decodeColorField(raw, path+"/color", nodeID, issues); fieldOK {
			spec.Color = value
		}
	}
	if raw, ok := m["align"]; ok {
		value, fieldOK := decodeStringRaw(raw, path+"/align", nodeID, issues)
		if fieldOK && !isAllowedString(value, string(AlignLeft), string(AlignCenter), string(AlignRight)) {
			*issues = append(*issues, errorIssue(nodeID, path+"/align", "bad_enum", "align must be left, center, or right"))
		} else if fieldOK {
			spec.Align = Align(value)
		}
	}
	if raw, ok := m["valign"]; ok {
		value, fieldOK := decodeStringRaw(raw, path+"/valign", nodeID, issues)
		if fieldOK && !isAllowedString(value, string(VAlignTop), string(VAlignMiddle), string(VAlignBottom)) {
			*issues = append(*issues, errorIssue(nodeID, path+"/valign", "bad_enum", "valign must be top, middle, or bottom"))
		} else if fieldOK {
			spec.VAlign = VAlign(value)
		}
	}
	if raw, ok := m["wrap"]; ok {
		if value, fieldOK := decodeBoolRaw(raw, path+"/wrap", nodeID, issues); fieldOK {
			spec.Wrap = value
		}
	}
	if raw, ok := m["max_lines"]; ok {
		if value, fieldOK := decodeIntRaw(raw, path+"/max_lines", nodeID, issues); fieldOK {
			spec.MaxLines = &value
			if value <= 0 {
				*issues = append(*issues, errorIssue(nodeID, path+"/max_lines", "out_of_range", "max_lines must be positive"))
			}
		}
	}
	if raw, ok := m["shrink_to_fit"]; ok {
		if value, fieldOK := decodeBoolRaw(raw, path+"/shrink_to_fit", nodeID, issues); fieldOK {
			spec.ShrinkToFit = value
		}
	}
	if raw, ok := m["shrink_min_size"]; ok {
		if value, fieldOK := decodeIntRaw(raw, path+"/shrink_min_size", nodeID, issues); fieldOK {
			spec.ShrinkMinSize = &value
			if value <= 0 {
				*issues = append(*issues, errorIssue(nodeID, path+"/shrink_min_size", "out_of_range", "shrink_min_size must be positive"))
			}
		}
	}
	if raw, ok := m["line_height"]; ok {
		if value, fieldOK := decodeFloatRaw(raw, path+"/line_height", nodeID, issues); fieldOK {
			spec.LineHeight = &value
			if value <= 0 {
				*issues = append(*issues, errorIssue(nodeID, path+"/line_height", "out_of_range", "line_height must be positive"))
			}
		}
	}
}

func parseGridNode(m map[string]json.RawMessage, path, nodeID, canvasBackground string, issues *[]Issue) *GridSpec {
	grid := &GridSpec{GapX: 24, GapY: 24}
	if raw, ok := m["columns"]; ok {
		if value, fieldOK := decodeIntRaw(raw, path+"/columns", nodeID, issues); fieldOK {
			grid.Columns = value
			if value <= 0 {
				*issues = append(*issues, errorIssue(nodeID, path+"/columns", "out_of_range", "columns must be positive"))
			}
		}
	} else {
		*issues = append(*issues, errorIssue(nodeID, path+"/columns", "missing_required", "columns is required for grid nodes"))
	}
	if raw, ok := m["rows"]; ok {
		if value, fieldOK := decodeIntRaw(raw, path+"/rows", nodeID, issues); fieldOK {
			grid.Rows = &value
			if value <= 0 {
				*issues = append(*issues, errorIssue(nodeID, path+"/rows", "out_of_range", "rows must be positive"))
			}
		}
	}
	parseNonNegativeIntField(m, "gap_x", path, nodeID, &grid.GapX, issues)
	parseNonNegativeIntField(m, "gap_y", path, nodeID, &grid.GapY, issues)
	if raw, ok := m["children"]; ok {
		grid.Children = parseAuthorNodes(raw, path+"/children", authorNodeAssigned, canvasBackground, issues)
		if len(grid.Children) == 0 {
			*issues = append(*issues, errorIssue(nodeID, path+"/children", "missing_required", "grid children must be non-empty"))
		}
	} else {
		*issues = append(*issues, errorIssue(nodeID, path+"/children", "missing_required", "children is required for grid nodes"))
	}
	if grid.Rows != nil && grid.Columns > 0 && *grid.Rows > 0 && len(grid.Children) > 0 && (*grid.Rows)*grid.Columns < len(grid.Children) {
		*issues = append(*issues, errorIssue(nodeID, path+"/rows", "out_of_range", "rows * columns must be at least the number of grid children"))
	}
	return grid
}

func parseNonNegativeIntField(m map[string]json.RawMessage, key, path, nodeID string, dst *int, issues *[]Issue) {
	if raw, ok := m[key]; ok {
		if value, fieldOK := decodeIntRaw(raw, path+"/"+key, nodeID, issues); fieldOK {
			*dst = value
			if value < 0 {
				*issues = append(*issues, errorIssue(nodeID, path+"/"+key, "out_of_range", key+" must be non-negative"))
			}
		}
	}
}

func parseListNode(m map[string]json.RawMessage, path, nodeID string, issues *[]Issue) *ListSpec {
	list := &ListSpec{Marker: "bullet", Font: Font{Family: FontFamilySans, Size: 26, Weight: FontWeightRegular}, Color: DefaultTextColor, ShrinkMinSize: DefaultShrinkMinSize, MarkerGap: 12, ItemGap: 8}
	if raw, ok := m["items"]; ok {
		list.Items = parseStringArray(raw, path+"/items", nodeID, "items", issues)
		if len(list.Items) == 0 {
			*issues = append(*issues, errorIssue(nodeID, path+"/items", "missing_required", "list items must be non-empty"))
		}
		for i, item := range list.Items {
			if item == "" {
				*issues = append(*issues, errorIssue(nodeID, fmt.Sprintf("%s/items/%d", path, i), "missing_required", "list item must not be empty"))
			}
		}
	} else {
		*issues = append(*issues, errorIssue(nodeID, path+"/items", "missing_required", "items is required for list nodes"))
	}
	if raw, ok := m["marker"]; ok {
		value, fieldOK := decodeStringRaw(raw, path+"/marker", nodeID, issues)
		if fieldOK && !isAllowedString(value, "bullet", "dash", "number", "check", "none") {
			*issues = append(*issues, errorIssue(nodeID, path+"/marker", "bad_enum", "marker must be bullet, dash, number, check, or none"))
		} else if fieldOK {
			list.Marker = value
		}
	}
	if raw, ok := m["font"]; ok {
		list.Font = parseFont(raw, path+"/font", nodeID, issues)
	}
	if raw, ok := m["color"]; ok {
		if value, fieldOK := decodeColorField(raw, path+"/color", nodeID, issues); fieldOK {
			list.Color = value
		}
	}
	if raw, ok := m["line_height"]; ok {
		if value, fieldOK := decodeFloatRaw(raw, path+"/line_height", nodeID, issues); fieldOK {
			list.LineHeight = &value
			if value <= 0 {
				*issues = append(*issues, errorIssue(nodeID, path+"/line_height", "out_of_range", "line_height must be positive"))
			}
		}
	}
	if raw, ok := m["shrink_to_fit"]; ok {
		if value, fieldOK := decodeBoolRaw(raw, path+"/shrink_to_fit", nodeID, issues); fieldOK {
			list.ShrinkToFit = value
		}
	}
	if raw, ok := m["shrink_min_size"]; ok {
		if value, fieldOK := decodeIntRaw(raw, path+"/shrink_min_size", nodeID, issues); fieldOK {
			list.ShrinkMinSize = value
			if value <= 0 {
				*issues = append(*issues, errorIssue(nodeID, path+"/shrink_min_size", "out_of_range", "shrink_min_size must be positive"))
			}
		}
	}
	parseNonNegativeIntField(m, "marker_gap", path, nodeID, &list.MarkerGap, issues)
	parseNonNegativeIntField(m, "item_gap", path, nodeID, &list.ItemGap, issues)
	return list
}

func parseStringArray(raw json.RawMessage, path, nodeID, fieldName string, issues *[]Issue) []string {
	if !rawStartsWith(raw, '[') {
		*issues = append(*issues, errorIssue(nodeID, path, "invalid_type", fieldName+" must be an array"))
		return nil
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(raw, &raws); err != nil {
		*issues = append(*issues, errorIssue(nodeID, path, "invalid_type", fieldName+" must be an array"))
		return nil
	}
	items := make([]string, 0, len(raws))
	for i, itemRaw := range raws {
		value, ok := decodeStringRaw(itemRaw, fmt.Sprintf("%s/%d", path, i), nodeID, issues)
		if ok {
			items = append(items, value)
		}
	}
	return items
}

func parseStackNode(m map[string]json.RawMessage, path, nodeID, canvasBackground string, issues *[]Issue) *StackSpec {
	stack := &StackSpec{Direction: "vertical", Gap: 24}
	if raw, ok := m["direction"]; ok {
		value, fieldOK := decodeStringRaw(raw, path+"/direction", nodeID, issues)
		if fieldOK && !isAllowedString(value, "vertical", "horizontal") {
			*issues = append(*issues, errorIssue(nodeID, path+"/direction", "bad_enum", "direction must be vertical or horizontal"))
		} else if fieldOK {
			stack.Direction = value
		}
	}
	parseNonNegativeIntField(m, "gap", path, nodeID, &stack.Gap, issues)
	if raw, ok := m["children"]; ok {
		stack.Children = parseStackChildren(raw, path+"/children", nodeID, stack.Direction, canvasBackground, issues)
		if len(stack.Children) == 0 {
			*issues = append(*issues, errorIssue(nodeID, path+"/children", "missing_required", "stack children must be non-empty"))
		}
	} else {
		*issues = append(*issues, errorIssue(nodeID, path+"/children", "missing_required", "children is required for stack nodes"))
	}
	return stack
}

func parseStackChildren(raw json.RawMessage, path, nodeID, direction, canvasBackground string, issues *[]Issue) []StackChildSpec {
	if !rawStartsWith(raw, '[') {
		*issues = append(*issues, errorIssue(nodeID, path, "invalid_type", "children must be an array"))
		return nil
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(raw, &raws); err != nil {
		*issues = append(*issues, errorIssue(nodeID, path, "invalid_type", "children must be an array"))
		return nil
	}
	children := make([]StackChildSpec, 0, len(raws))
	for i, childRaw := range raws {
		childPath := fmt.Sprintf("%s/%d", path, i)
		child, ok := parseStackChild(childRaw, childPath, nodeID, direction, canvasBackground, issues)
		if ok {
			children = append(children, child)
		}
	}
	return children
}

func parseStackChild(raw json.RawMessage, path, nodeID, direction, canvasBackground string, issues *[]Issue) (StackChildSpec, bool) {
	m, ok := decodeObjectMessage(raw, path, nodeID, "stack child must be an object", issues)
	if !ok {
		return StackChildSpec{}, false
	}
	allowed := allowedSet("node")
	sizeKey := "height"
	if direction == "horizontal" {
		sizeKey = "width"
	}
	allowed[sizeKey] = true
	addUnknownFields(m, path, allowed, nodeID, issues)
	child := StackChildSpec{Path: path}
	if raw, ok := m[sizeKey]; ok {
		if value, fieldOK := decodeIntRaw(raw, path+"/"+sizeKey, nodeID, issues); fieldOK {
			child.Size = value
			if value <= 0 {
				*issues = append(*issues, errorIssue(nodeID, path+"/"+sizeKey, "out_of_range", sizeKey+" must be positive"))
			}
		}
	} else {
		*issues = append(*issues, errorIssue(nodeID, path+"/"+sizeKey, "missing_required", sizeKey+" is required for stack children"))
	}
	if raw, ok := m["node"]; ok {
		child.Node = parseAuthorNode(raw, path+"/node", authorNodeAssigned, canvasBackground, issues)
	} else {
		*issues = append(*issues, errorIssue(nodeID, path+"/node", "missing_required", "node is required for stack children"))
	}
	return child, true
}

func parseSectionNode(m map[string]json.RawMessage, path, nodeID, canvasBackground string, issues *[]Issue) *SectionSpec {
	section := &SectionSpec{TitleFont: Font{Family: FontFamilySans, Size: 42, Weight: FontWeightBold}, TitleColor: DefaultTextColor, TitleAlign: AlignLeft, TitleVAlign: VAlignMiddle, TitleHeight: 72, Gap: 24, Rule: true, RuleStroke: DefaultTextColor, RuleStrokeWidth: 2, RuleHeight: 4}
	if raw, ok := m["title"]; ok {
		section.Title, _ = decodeStringRaw(raw, path+"/title", nodeID, issues)
	} else {
		*issues = append(*issues, errorIssue(nodeID, path+"/title", "missing_required", "title is required for section nodes"))
	}
	if raw, ok := m["title_font"]; ok {
		section.TitleFont = parseFont(raw, path+"/title_font", nodeID, issues)
	}
	if raw, ok := m["title_color"]; ok {
		if value, fieldOK := decodeColorField(raw, path+"/title_color", nodeID, issues); fieldOK {
			section.TitleColor = value
		}
	}
	if raw, ok := m["title_align"]; ok {
		value, fieldOK := decodeStringRaw(raw, path+"/title_align", nodeID, issues)
		if fieldOK && !isAllowedString(value, string(AlignLeft), string(AlignCenter), string(AlignRight)) {
			*issues = append(*issues, errorIssue(nodeID, path+"/title_align", "bad_enum", "title_align must be left, center, or right"))
		} else if fieldOK {
			section.TitleAlign = Align(value)
		}
	}
	if raw, ok := m["title_valign"]; ok {
		value, fieldOK := decodeStringRaw(raw, path+"/title_valign", nodeID, issues)
		if fieldOK && !isAllowedString(value, string(VAlignTop), string(VAlignMiddle), string(VAlignBottom)) {
			*issues = append(*issues, errorIssue(nodeID, path+"/title_valign", "bad_enum", "title_valign must be top, middle, or bottom"))
		} else if fieldOK {
			section.TitleVAlign = VAlign(value)
		}
	}
	parsePositiveIntField(m, "title_height", path, nodeID, &section.TitleHeight, issues)
	parseNonNegativeIntField(m, "gap", path, nodeID, &section.Gap, issues)
	if raw, ok := m["rule"]; ok {
		if value, fieldOK := decodeBoolRaw(raw, path+"/rule", nodeID, issues); fieldOK {
			section.Rule = value
		}
	}
	if raw, ok := m["rule_stroke"]; ok {
		if value, fieldOK := decodeColorField(raw, path+"/rule_stroke", nodeID, issues); fieldOK {
			section.RuleStroke = value
		}
	}
	parsePositiveIntField(m, "rule_stroke_width", path, nodeID, &section.RuleStrokeWidth, issues)
	parsePositiveIntField(m, "rule_height", path, nodeID, &section.RuleHeight, issues)
	if raw, ok := m["children"]; ok {
		section.Children = parseStackChildren(raw, path+"/children", nodeID, "vertical", canvasBackground, issues)
	}
	return section
}

func parsePositiveIntField(m map[string]json.RawMessage, key, path, nodeID string, dst *int, issues *[]Issue) {
	if raw, ok := m[key]; ok {
		if value, fieldOK := decodeIntRaw(raw, path+"/"+key, nodeID, issues); fieldOK {
			*dst = value
			if value <= 0 {
				*issues = append(*issues, errorIssue(nodeID, path+"/"+key, "out_of_range", key+" must be positive"))
			}
		}
	}
}

func validateAuthorScene(scene AuthorScene) []Issue {
	var issues []Issue
	if scene.Schema != "" && scene.Schema != SchemaURLV1 {
		issues = append(issues, errorIssue("", "/$schema", "bad_schema", "unsupported schema identifier"))
	}
	if scene.Version != "" && scene.Version != SceneVersionV1 {
		issues = append(issues, errorIssue("", "/version", "bad_enum", fmt.Sprintf("version must be %q", SceneVersionV1)))
	}
	if scene.Canvas.Width != CanvasWidth {
		issues = append(issues, errorIssue("", "/canvas/width", "bad_canvas", fmt.Sprintf("canvas.width must be %d", CanvasWidth)))
	}
	if scene.Canvas.Height != CanvasHeight {
		issues = append(issues, errorIssue("", "/canvas/height", "bad_canvas", fmt.Sprintf("canvas.height must be %d", CanvasHeight)))
	}
	if scene.Canvas.Background != "" && !isHexColor(scene.Canvas.Background) {
		issues = append(issues, errorIssue("", "/canvas/background", "bad_color", "background must be a #rrggbb color"))
	}
	seen := map[string]string{}
	validateAuthorNodeIDs(scene.Nodes, seen, &issues)
	return issues
}

func validateAuthorNodeIDs(nodes []AuthorNode, seen map[string]string, issues *[]Issue) {
	for _, node := range nodes {
		if node.ID != "" {
			if first, ok := seen[node.ID]; ok {
				*issues = append(*issues, errorIssue(node.ID, node.Path+"/id", "duplicate_id", fmt.Sprintf("node id %q duplicates %s/id", node.ID, first)))
			} else {
				seen[node.ID] = node.Path
			}
		}
		if node.Grid != nil {
			validateAuthorNodeIDs(node.Grid.Children, seen, issues)
		}
		if node.Stack != nil {
			validateStackChildNodeIDs(node.Stack.Children, seen, issues)
		}
		if node.Section != nil {
			validateStackChildNodeIDs(node.Section.Children, seen, issues)
		}
	}
}

func validateStackChildNodeIDs(children []StackChildSpec, seen map[string]string, issues *[]Issue) {
	for _, child := range children {
		validateAuthorNodeIDs([]AuthorNode{child.Node}, seen, issues)
	}
}

func allowedAuthorNodeFields(typ NodeType) map[string]bool {
	base := []string{"id", "type", "box"}
	switch typ {
	case NodeTypeText:
		return allowedSet(append(base, "text", "font", "align", "valign", "color", "wrap", "max_lines", "shrink_to_fit", "shrink_min_size", "line_height")...)
	case NodeTypeRect:
		return allowedSet(append(base, "fill", "stroke", "stroke_width", "radius")...)
	case NodeTypeLine:
		return allowedSet(append(base, "from", "to", "stroke", "stroke_width")...)
	case NodeTypeImage:
		return allowedSet(append(base, "src", "fit", "position", "background")...)
	case NodeTypeRule:
		return allowedSet(append(base, "orientation", "stroke", "stroke_width", "inset_start", "inset_end")...)
	case NodeTypeCard:
		return allowedSet(append(base, "fill", "stroke", "stroke_width", "radius", "padding", "header_gap", "header", "body")...)
	case NodeTypeGrid:
		return allowedSet(append(base, "columns", "rows", "gap_x", "gap_y", "children")...)
	case NodeTypeList:
		return allowedSet(append(base, "items", "marker", "font", "color", "line_height", "shrink_to_fit", "shrink_min_size", "marker_gap", "item_gap")...)
	case NodeTypeStack:
		return allowedSet(append(base, "direction", "gap", "children")...)
	case NodeTypeSection:
		return allowedSet(append(base, "title", "title_font", "title_color", "title_align", "title_valign", "title_height", "gap", "rule", "rule_stroke", "rule_stroke_width", "rule_height", "children")...)
	default:
		return nil
	}
}

func allKnownAuthorNodeFields() map[string]bool {
	return allowedSet("id", "type", "box", "text", "font", "align", "valign", "color", "wrap", "max_lines", "shrink_to_fit", "shrink_min_size", "line_height", "fill", "stroke", "stroke_width", "radius", "from", "to", "src", "fit", "position", "background", "orientation", "inset_start", "inset_end", "padding", "header_gap", "header", "body", "columns", "rows", "gap_x", "gap_y", "children", "items", "marker", "marker_gap", "item_gap", "direction", "gap", "title", "title_font", "title_color", "title_align", "title_valign", "title_height", "rule", "rule_stroke", "rule_stroke_width", "rule_height")
}

func pathLeafName(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}
