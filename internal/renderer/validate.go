package renderer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

// ValidateSceneJSON parses scene JSON with strict v1 rules and returns the
// parsed scene plus a report-ready issue list. It intentionally performs only
// structural and contract validation; pixel drawing, asset decoding, and text
// overflow checks happen in later renderer stages.
func ValidateSceneJSON(data []byte) (AuthorScene, Report) {
	author, issues := parseAuthorSceneStrict(data)
	return author, NewValidationReport(author, issues)
}

func parseCanvas(raw json.RawMessage, issues *[]Issue) Canvas {
	canvas := Canvas{Width: CanvasWidth, Height: CanvasHeight, Background: DefaultCanvasBackground}
	m, ok := decodeObjectMessage(raw, "/canvas", "", "canvas must be an object", issues)
	if !ok {
		return canvas
	}
	addUnknownFields(m, "/canvas", allowedSet("width", "height", "background"), "", issues)
	if raw, ok := m["width"]; ok {
		if value, fieldOK := decodeIntRaw(raw, "/canvas/width", "", issues); fieldOK {
			canvas.Width = value
		}
	}
	if raw, ok := m["height"]; ok {
		if value, fieldOK := decodeIntRaw(raw, "/canvas/height", "", issues); fieldOK {
			canvas.Height = value
		}
	}
	if raw, ok := m["background"]; ok {
		if value, fieldOK := decodeStringRaw(raw, "/canvas/background", "", issues); fieldOK {
			canvas.Background = value
		}
	}
	return canvas
}

func parseBox(raw json.RawMessage, path, nodeID string, issues *[]Issue) Box {
	var box Box
	m, ok := decodeObjectMessage(raw, path, nodeID, "box must be an object", issues)
	if !ok {
		return box
	}
	addUnknownFields(m, path, allowedSet("x", "y", "w", "h"), nodeID, issues)
	xOK, yOK, wOK, hOK := false, false, false, false
	if raw, ok := m["x"]; ok {
		box.X, xOK = decodeIntRaw(raw, path+"/x", nodeID, issues)
	} else {
		*issues = append(*issues, errorIssue(nodeID, path+"/x", "missing_required", "box x is required"))
	}
	if raw, ok := m["y"]; ok {
		box.Y, yOK = decodeIntRaw(raw, path+"/y", nodeID, issues)
	} else {
		*issues = append(*issues, errorIssue(nodeID, path+"/y", "missing_required", "box y is required"))
	}
	if raw, ok := m["w"]; ok {
		box.W, wOK = decodeIntRaw(raw, path+"/w", nodeID, issues)
	} else {
		*issues = append(*issues, errorIssue(nodeID, path+"/w", "missing_required", "box w is required"))
	}
	if raw, ok := m["h"]; ok {
		box.H, hOK = decodeIntRaw(raw, path+"/h", nodeID, issues)
	} else {
		*issues = append(*issues, errorIssue(nodeID, path+"/h", "missing_required", "box h is required"))
	}
	if xOK && box.X < 0 {
		*issues = append(*issues, errorIssue(nodeID, path+"/x", "out_of_range", "box x must be non-negative"))
	}
	if yOK && box.Y < 0 {
		*issues = append(*issues, errorIssue(nodeID, path+"/y", "out_of_range", "box y must be non-negative"))
	}
	if wOK && box.W <= 0 {
		*issues = append(*issues, errorIssue(nodeID, path+"/w", "out_of_range", "box w must be positive"))
	}
	if hOK && box.H <= 0 {
		*issues = append(*issues, errorIssue(nodeID, path+"/h", "out_of_range", "box h must be positive"))
	}
	if xOK && yOK && wOK && hOK && box.X >= 0 && box.Y >= 0 && box.W > 0 && box.H > 0 {
		if box.X > CanvasWidth-box.W || box.Y > CanvasHeight-box.H {
			*issues = append(*issues, errorIssue(nodeID, path, "box_out_of_bounds", fmt.Sprintf("box must be fully inside %dx%d canvas", CanvasWidth, CanvasHeight)))
		}
	}
	return box
}

func parsePoint(raw json.RawMessage, path, nodeID string, issues *[]Issue) (Point, bool) {
	var point Point
	m, ok := decodeObjectMessage(raw, path, nodeID, "point must be an object", issues)
	if !ok {
		return point, false
	}
	addUnknownFields(m, path, allowedSet("x", "y"), nodeID, issues)
	ok = true
	if raw, exists := m["x"]; exists {
		var fieldOK bool
		point.X, fieldOK = decodeIntRaw(raw, path+"/x", nodeID, issues)
		ok = ok && fieldOK
	} else {
		*issues = append(*issues, errorIssue(nodeID, path+"/x", "missing_required", "point x is required"))
		ok = false
	}
	if raw, exists := m["y"]; exists {
		var fieldOK bool
		point.Y, fieldOK = decodeIntRaw(raw, path+"/y", nodeID, issues)
		ok = ok && fieldOK
	} else {
		*issues = append(*issues, errorIssue(nodeID, path+"/y", "missing_required", "point y is required"))
		ok = false
	}
	return point, ok
}

func parseTextNode(m map[string]json.RawMessage, path, nodeID string, node *Node, issues *[]Issue) {
	if raw, ok := m["text"]; ok {
		node.Text, _ = decodeStringRaw(raw, path+"/text", nodeID, issues)
	} else {
		*issues = append(*issues, errorIssue(nodeID, path+"/text", "missing_required", "text is required for text nodes"))
	}
	if raw, ok := m["font"]; ok {
		font := parseFont(raw, path+"/font", nodeID, issues)
		node.Font = &font
	} else {
		*issues = append(*issues, errorIssue(nodeID, path+"/font/size", "missing_required", "font.size is required for text nodes"))
	}
	if raw, ok := m["align"]; ok {
		value, fieldOK := decodeStringRaw(raw, path+"/align", nodeID, issues)
		if fieldOK && !isAllowedString(value, string(AlignLeft), string(AlignCenter), string(AlignRight)) {
			*issues = append(*issues, errorIssue(nodeID, path+"/align", "bad_enum", "align must be left, center, or right"))
		} else if fieldOK {
			node.Align = Align(value)
		}
	}
	if raw, ok := m["valign"]; ok {
		value, fieldOK := decodeStringRaw(raw, path+"/valign", nodeID, issues)
		if fieldOK && !isAllowedString(value, string(VAlignTop), string(VAlignMiddle), string(VAlignBottom)) {
			*issues = append(*issues, errorIssue(nodeID, path+"/valign", "bad_enum", "valign must be top, middle, or bottom"))
		} else if fieldOK {
			node.VAlign = VAlign(value)
		}
	}
	if raw, ok := m["color"]; ok {
		node.Color, _ = decodeColorField(raw, path+"/color", nodeID, issues)
	}
	if raw, ok := m["wrap"]; ok {
		value, fieldOK := decodeBoolRaw(raw, path+"/wrap", nodeID, issues)
		if fieldOK {
			node.Wrap = &value
		}
	}
	if raw, ok := m["max_lines"]; ok {
		value, fieldOK := decodeIntRaw(raw, path+"/max_lines", nodeID, issues)
		if fieldOK {
			node.MaxLines = &value
			if value <= 0 {
				*issues = append(*issues, errorIssue(nodeID, path+"/max_lines", "out_of_range", "max_lines must be positive"))
			}
		}
	}
	if raw, ok := m["shrink_to_fit"]; ok {
		value, fieldOK := decodeBoolRaw(raw, path+"/shrink_to_fit", nodeID, issues)
		if fieldOK {
			node.ShrinkToFit = &value
		}
	}
	if raw, ok := m["shrink_min_size"]; ok {
		value, fieldOK := decodeIntRaw(raw, path+"/shrink_min_size", nodeID, issues)
		if fieldOK {
			node.ShrinkMinSize = &value
			if value <= 0 {
				*issues = append(*issues, errorIssue(nodeID, path+"/shrink_min_size", "out_of_range", "shrink_min_size must be positive"))
			}
		}
	}
	if raw, ok := m["line_height"]; ok {
		value, fieldOK := decodeFloatRaw(raw, path+"/line_height", nodeID, issues)
		if fieldOK {
			node.LineHeight = &value
			if value <= 0 {
				*issues = append(*issues, errorIssue(nodeID, path+"/line_height", "out_of_range", "line_height must be positive"))
			}
		}
	}
}

func parseFont(raw json.RawMessage, path, nodeID string, issues *[]Issue) Font {
	font := Font{Family: FontFamilySans, Weight: FontWeightRegular}
	m, ok := decodeObjectMessage(raw, path, nodeID, "font must be an object", issues)
	if !ok {
		return font
	}
	addUnknownFields(m, path, allowedSet("family", "size", "weight"), nodeID, issues)
	if raw, ok := m["family"]; ok {
		value, fieldOK := decodeStringRaw(raw, path+"/family", nodeID, issues)
		if fieldOK && !isAllowedString(value, string(FontFamilySans), string(FontFamilySerif), string(FontFamilyMono)) {
			*issues = append(*issues, errorIssue(nodeID, path+"/family", "bad_enum", "font.family must be sans, serif, or mono"))
		} else if fieldOK {
			font.Family = FontFamily(value)
		}
	}
	if raw, ok := m["size"]; ok {
		value, fieldOK := decodeIntRaw(raw, path+"/size", nodeID, issues)
		if fieldOK {
			font.Size = value
			if value <= 0 || value > MaxFontSize {
				*issues = append(*issues, errorIssue(nodeID, path+"/size", "out_of_range", fmt.Sprintf("font.size must be between 1 and %d", MaxFontSize)))
			}
		}
	} else {
		*issues = append(*issues, errorIssue(nodeID, path+"/size", "missing_required", "font.size is required"))
	}
	if raw, ok := m["weight"]; ok {
		value, fieldOK := decodeStringRaw(raw, path+"/weight", nodeID, issues)
		if fieldOK && !isAllowedString(value, string(FontWeightRegular), string(FontWeightBold)) {
			*issues = append(*issues, errorIssue(nodeID, path+"/weight", "bad_enum", "font.weight must be regular or bold"))
		} else if fieldOK {
			font.Weight = FontWeight(value)
		}
	}
	return font
}

func parseRectNode(m map[string]json.RawMessage, path, nodeID string, node *Node, issues *[]Issue) {
	if raw, ok := m["fill"]; ok {
		node.Fill, _ = decodeColorField(raw, path+"/fill", nodeID, issues)
	}
	if raw, ok := m["stroke"]; ok {
		node.Stroke, _ = decodeColorField(raw, path+"/stroke", nodeID, issues)
	}
	if raw, ok := m["stroke_width"]; ok {
		value, fieldOK := decodeIntRaw(raw, path+"/stroke_width", nodeID, issues)
		if fieldOK {
			node.StrokeWidth = &value
			if value < 0 {
				*issues = append(*issues, errorIssue(nodeID, path+"/stroke_width", "out_of_range", "stroke_width must be non-negative"))
			}
		}
	}
	if raw, ok := m["radius"]; ok {
		value, fieldOK := decodeIntRaw(raw, path+"/radius", nodeID, issues)
		if fieldOK {
			node.Radius = &value
			if value < 0 {
				*issues = append(*issues, errorIssue(nodeID, path+"/radius", "out_of_range", "radius must be non-negative"))
			} else if node.Box.W > 0 && node.Box.H > 0 && value > minInt(node.Box.W, node.Box.H)/2 {
				*issues = append(*issues, errorIssue(nodeID, path+"/radius", "out_of_range", "radius must be at most half of the smaller box dimension"))
			}
		}
	}
}

func parseLineNode(m map[string]json.RawMessage, path, nodeID string, node *Node, issues *[]Issue) {
	fromOK, toOK := false, false
	if raw, ok := m["from"]; ok {
		from, fieldOK := parsePoint(raw, path+"/from", nodeID, issues)
		node.From = &from
		fromOK = fieldOK
	} else {
		*issues = append(*issues, errorIssue(nodeID, path+"/from", "missing_required", "from is required for line nodes"))
	}
	if raw, ok := m["to"]; ok {
		to, fieldOK := parsePoint(raw, path+"/to", nodeID, issues)
		node.To = &to
		toOK = fieldOK
	} else {
		*issues = append(*issues, errorIssue(nodeID, path+"/to", "missing_required", "to is required for line nodes"))
	}
	if raw, ok := m["stroke"]; ok {
		node.Stroke, _ = decodeColorField(raw, path+"/stroke", nodeID, issues)
	}
	if raw, ok := m["stroke_width"]; ok {
		value, fieldOK := decodeIntRaw(raw, path+"/stroke_width", nodeID, issues)
		if fieldOK {
			node.StrokeWidth = &value
			if value <= 0 {
				*issues = append(*issues, errorIssue(nodeID, path+"/stroke_width", "out_of_range", "stroke_width must be positive for line nodes"))
			}
		}
	}
	if fromOK {
		validatePointInsideBox(*node.From, node.Box, path+"/from", nodeID, issues)
	}
	if toOK {
		validatePointInsideBox(*node.To, node.Box, path+"/to", nodeID, issues)
	}
}

func validatePointInsideBox(point Point, box Box, path, nodeID string, issues *[]Issue) {
	if box.W <= 0 || box.H <= 0 {
		return
	}
	if point.X < 0 || point.Y < 0 || point.X >= box.W || point.Y >= box.H {
		*issues = append(*issues, errorIssue(nodeID, path, "point_out_of_bounds", "line point must be inside the node box and relative to its origin"))
	}
}

func validateImageSrc(src, path, nodeID string, issues *[]Issue) {
	if src == "" {
		*issues = append(*issues, errorIssue(nodeID, path, "missing_required", "src must not be empty"))
		return
	}
	lower := strings.ToLower(src)
	if strings.Contains(lower, "://") {
		*issues = append(*issues, errorIssue(nodeID, path, "bad_asset", "image src must be a local PNG or JPEG path, not a URL"))
	}
	if strings.HasPrefix(lower, "data:") {
		*issues = append(*issues, errorIssue(nodeID, path, "bad_asset", "data: image URIs are not supported in v1"))
	}
	ext := filepath.Ext(lower)
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" {
		*issues = append(*issues, errorIssue(nodeID, path, "bad_asset", "image src must point to a .png, .jpg, or .jpeg file"))
	}
}

func decodeObjectRaw(data []byte, path, nodeID, message string, issues *[]Issue) (map[string]json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		*issues = append(*issues, errorIssue(nodeID, path, "invalid_json", fmt.Sprintf("%s: EOF", message)))
		return nil, false
	}
	if trimmed[0] != '{' {
		*issues = append(*issues, errorIssue(nodeID, path, "invalid_type", message))
		return nil, false
	}
	var m map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		*issues = append(*issues, errorIssue(nodeID, path, "invalid_json", fmt.Sprintf("%s: %v", message, err)))
		return nil, false
	}
	if m == nil {
		*issues = append(*issues, errorIssue(nodeID, path, "invalid_type", message))
		return nil, false
	}
	return m, true
}

func decodeObjectMessage(raw json.RawMessage, path, nodeID, message string, issues *[]Issue) (map[string]json.RawMessage, bool) {
	return decodeObjectRaw(raw, path, nodeID, message, issues)
}

func rawStartsWith(raw json.RawMessage, b byte) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == b
}

func hasTrailingJSONValue(data []byte) bool {
	dec := json.NewDecoder(bytes.NewReader(data))
	var v any
	if err := dec.Decode(&v); err != nil {
		return false
	}
	if err := dec.Decode(&v); err == io.EOF {
		return false
	}
	return true
}

func decodeStringRaw(raw json.RawMessage, path, nodeID string, issues *[]Issue) (string, bool) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil || rawStartsWith(raw, 'n') {
		*issues = append(*issues, errorIssue(nodeID, path, "invalid_type", "must be a string"))
		return "", false
	}
	return value, true
}

func decodeIntRaw(raw json.RawMessage, path, nodeID string, issues *[]Issue) (int, bool) {
	var value int
	if err := json.Unmarshal(raw, &value); err != nil || rawStartsWith(raw, 'n') {
		*issues = append(*issues, errorIssue(nodeID, path, "invalid_type", "must be an integer"))
		return 0, false
	}
	return value, true
}

func decodeBoolRaw(raw json.RawMessage, path, nodeID string, issues *[]Issue) (bool, bool) {
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil || rawStartsWith(raw, 'n') {
		*issues = append(*issues, errorIssue(nodeID, path, "invalid_type", "must be a boolean"))
		return false, false
	}
	return value, true
}

func decodeFloatRaw(raw json.RawMessage, path, nodeID string, issues *[]Issue) (float64, bool) {
	var value float64
	if err := json.Unmarshal(raw, &value); err != nil || rawStartsWith(raw, 'n') {
		*issues = append(*issues, errorIssue(nodeID, path, "invalid_type", "must be a number"))
		return 0, false
	}
	return value, true
}

func decodeColorField(raw json.RawMessage, path, nodeID string, issues *[]Issue) (string, bool) {
	value, ok := decodeStringRaw(raw, path, nodeID, issues)
	if !ok {
		return "", false
	}
	if !isHexColor(value) {
		*issues = append(*issues, errorIssue(nodeID, path, "bad_color", "must be a #rrggbb color"))
		return value, false
	}
	return value, true
}

func isHexColor(s string) bool {
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	for _, r := range s[1:] {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func addUnknownFields(m map[string]json.RawMessage, basePath string, allowed map[string]bool, nodeID string, issues *[]Issue) {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !allowed[key] {
			*issues = append(*issues, errorIssue(nodeID, joinPath(basePath, key), "unknown_field", fmt.Sprintf("unknown field %q", key)))
		}
	}
}

func allowedSet(keys ...string) map[string]bool {
	m := make(map[string]bool, len(keys))
	for _, key := range keys {
		m[key] = true
	}
	return m
}

func joinPath(base, key string) string {
	escaped := strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
	if base == "" {
		return "/" + escaped
	}
	return base + "/" + escaped
}

func isAllowedString(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func errorIssue(nodeID, path, code, message string) Issue {
	return Issue{Severity: SeverityError, Node: nodeID, Path: path, Code: code, Message: message}
}
