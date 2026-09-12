package renderer

import (
	"embed"
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"sync"
	"unicode"

	xfont "golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

//go:embed fonts/*.ttf
var fontFS embed.FS

type fontKey struct {
	family FontFamily
	weight FontWeight
}

var (
	parsedFontsOnce sync.Once
	parsedFonts     map[fontKey]*opentype.Font
	parsedFontsErr  error
)

type textRenderResult struct {
	issues         []Issue
	actualFontSize int
	lines          int
}

type textLayout struct {
	fontSize    int
	lines       []textLine
	ascent      int
	descent     int
	lineAdvance int
	totalHeight int
	fits        bool
	reason      string
}

type textLine struct {
	text    string
	widthPx int
}

func renderText(dst *image.Gray, node Node, idx int) textRenderResult {
	if node.Font == nil {
		return textRenderResult{issues: []Issue{errorIssue(node.ID, fmt.Sprintf("/nodes/%d/font/size", idx), "missing_required", "font.size is required for text nodes")}}
	}
	fontSpec := normalizedFont(*node.Font)
	if fontSpec.Size <= 0 {
		return textRenderResult{issues: []Issue{errorIssue(node.ID, fmt.Sprintf("/nodes/%d/font/size", idx), "out_of_range", "font.size must be positive")}}
	}

	gray, err := HexColorToGray(textColor(node))
	if err != nil {
		return textRenderResult{issues: []Issue{errorIssue(node.ID, fmt.Sprintf("/nodes/%d/color", idx), "bad_color", err.Error())}}
	}

	wrap := boolDefault(node.Wrap, false)
	shrinkToFit := boolDefault(node.ShrinkToFit, false)
	shrinkMinSize := DefaultShrinkMinSize
	if node.ShrinkMinSize != nil {
		shrinkMinSize = *node.ShrinkMinSize
	}
	if shrinkMinSize <= 0 {
		return textRenderResult{issues: []Issue{errorIssue(node.ID, fmt.Sprintf("/nodes/%d/shrink_min_size", idx), "out_of_range", "shrink_min_size must be positive")}}
	}
	if shrinkMinSize > fontSpec.Size {
		shrinkMinSize = fontSpec.Size
	}
	lineHeight := DefaultLineHeight
	if node.LineHeight != nil {
		lineHeight = *node.LineHeight
	}
	if lineHeight <= 0 {
		return textRenderResult{issues: []Issue{errorIssue(node.ID, fmt.Sprintf("/nodes/%d/line_height", idx), "out_of_range", "line_height must be positive")}}
	}

	// Glyph coverage does not depend on font size. Check it once before the
	// shrink loop so missing glyphs are reported as contract errors, not as a
	// fit problem.
	coverageFace, err := newFace(fontSpec.Family, fontSpec.Weight, fontSpec.Size)
	if err != nil {
		return textRenderResult{issues: []Issue{errorIssue(node.ID, fmt.Sprintf("/nodes/%d/font", idx), "font_load_failed", err.Error())}}
	}
	if issue := validateGlyphCoverage(coverageFace, node.Text, node.ID, fmt.Sprintf("/nodes/%d/text", idx)); issue != nil {
		_ = coverageFace.Close()
		return textRenderResult{issues: []Issue{*issue}}
	}
	_ = coverageFace.Close()

	minSize := fontSpec.Size
	if shrinkToFit {
		minSize = shrinkMinSize
	}

	var chosen textLayout
	var last textLayout
	for size := fontSpec.Size; size >= minSize; size-- {
		face, err := newFace(fontSpec.Family, fontSpec.Weight, size)
		if err != nil {
			return textRenderResult{issues: []Issue{errorIssue(node.ID, fmt.Sprintf("/nodes/%d/font", idx), "font_load_failed", err.Error())}}
		}
		layout := layoutText(face, node.Text, node.Box, size, lineHeight, wrap, node.MaxLines)
		_ = face.Close()
		last = layout
		if layout.fits {
			chosen = layout
			break
		}
		if !shrinkToFit {
			break
		}
	}

	if !chosen.fits {
		message := last.reason
		if message == "" {
			message = "text does not fit inside node box"
		}
		if shrinkToFit {
			message = fmt.Sprintf("%s; text still does not fit at shrink_min_size=%d", message, minSize)
		}
		return textRenderResult{issues: []Issue{errorIssue(node.ID, fmt.Sprintf("/nodes/%d/text", idx), "text_overflow", message)}}
	}

	face, err := newFace(fontSpec.Family, fontSpec.Weight, chosen.fontSize)
	if err != nil {
		return textRenderResult{issues: []Issue{errorIssue(node.ID, fmt.Sprintf("/nodes/%d/font", idx), "font_load_failed", err.Error())}}
	}
	defer face.Close()

	if issue := validateLaidOutBounds(face, node, idx, chosen); issue != nil {
		return textRenderResult{issues: []Issue{*issue}}
	}
	drawTextLayout(dst, face, node, chosen, gray)

	issues := []Issue{}
	if chosen.fontSize != fontSpec.Size {
		issues = append(issues, noticeIssue(node.ID, fmt.Sprintf("/nodes/%d/font/size", idx), "text_shrunk", fmt.Sprintf("font size reduced from %d to %d because shrink_to_fit=true", fontSpec.Size, chosen.fontSize)))
	}
	return textRenderResult{issues: issues, actualFontSize: chosen.fontSize, lines: len(chosen.lines)}
}

func normalizedFont(font Font) Font {
	if font.Family == "" {
		font.Family = FontFamilySans
	}
	if font.Weight == "" {
		font.Weight = FontWeightRegular
	}
	return font
}

func textColor(node Node) string {
	if node.Color == "" {
		return DefaultTextColor
	}
	return node.Color
}

func boolDefault(v *bool, def bool) bool {
	if v == nil {
		return def
	}
	return *v
}

func newFace(family FontFamily, weight FontWeight, size int) (xfont.Face, error) {
	font, err := bundledOpenTypeFont(family, weight)
	if err != nil {
		return nil, err
	}
	return opentype.NewFace(font, &opentype.FaceOptions{
		Size:    float64(size),
		DPI:     72,
		Hinting: xfont.HintingFull,
	})
}

func bundledOpenTypeFont(family FontFamily, weight FontWeight) (*opentype.Font, error) {
	parsedFontsOnce.Do(func() {
		parsedFonts = make(map[fontKey]*opentype.Font)
		entries := []struct {
			key  fontKey
			path string
		}{
			{fontKey{FontFamilySans, FontWeightRegular}, "fonts/NotoSans-Regular.ttf"},
			{fontKey{FontFamilySans, FontWeightBold}, "fonts/NotoSans-Bold.ttf"},
			{fontKey{FontFamilySerif, FontWeightRegular}, "fonts/NotoSerif-Regular.ttf"},
			{fontKey{FontFamilySerif, FontWeightBold}, "fonts/NotoSerif-Bold.ttf"},
			{fontKey{FontFamilyMono, FontWeightRegular}, "fonts/NotoSansMono-Regular.ttf"},
			{fontKey{FontFamilyMono, FontWeightBold}, "fonts/NotoSansMono-Bold.ttf"},
		}
		for _, entry := range entries {
			data, err := fontFS.ReadFile(entry.path)
			if err != nil {
				parsedFontsErr = fmt.Errorf("read bundled font %s: %w", entry.path, err)
				return
			}
			font, err := opentype.Parse(data)
			if err != nil {
				parsedFontsErr = fmt.Errorf("parse bundled font %s: %w", entry.path, err)
				return
			}
			parsedFonts[entry.key] = font
		}
	})
	if parsedFontsErr != nil {
		return nil, parsedFontsErr
	}
	key := fontKey{family: family, weight: weight}
	font := parsedFonts[key]
	if font == nil {
		return nil, fmt.Errorf("unsupported font alias family=%q weight=%q", family, weight)
	}
	return font, nil
}

func validateGlyphCoverage(face xfont.Face, text, nodeID, path string) *Issue {
	for _, r := range text {
		switch r {
		case '\n', '\r', '\t':
			continue
		}
		if unicode.IsControl(r) {
			issue := errorIssue(nodeID, path, "unsupported_glyph", fmt.Sprintf("text contains unsupported control character U+%04X", r))
			return &issue
		}
		if _, ok := face.GlyphAdvance(r); !ok {
			issue := errorIssue(nodeID, path, "unsupported_glyph", fmt.Sprintf("font does not contain glyph U+%04X", r))
			return &issue
		}
	}
	return nil
}

func layoutText(face xfont.Face, text string, box Box, fontSize int, lineHeight float64, wrap bool, maxLines *int) textLayout {
	lines := buildTextLines(face, text, box.W, wrap)
	layout := textLayout{fontSize: fontSize, lines: lines}
	metrics := face.Metrics()
	layout.ascent = fixedCeil(metrics.Ascent)
	layout.descent = fixedCeil(metrics.Descent)
	layout.lineAdvance = int(math.Ceil(float64(fontSize) * lineHeight))
	if layout.lineAdvance < 1 {
		layout.lineAdvance = 1
	}
	if len(lines) == 0 {
		layout.lines = []textLine{{}}
		lines = layout.lines
	}
	layout.totalHeight = layout.ascent + layout.descent + (len(lines)-1)*layout.lineAdvance

	if maxLines != nil && len(lines) > *maxLines {
		layout.reason = fmt.Sprintf("text wraps to %d lines, exceeding max_lines=%d", len(lines), *maxLines)
		return layout
	}
	for _, line := range lines {
		if line.widthPx > box.W {
			layout.reason = fmt.Sprintf("line width %d exceeds box width %d", line.widthPx, box.W)
			return layout
		}
	}
	if layout.totalHeight > box.H {
		layout.reason = fmt.Sprintf("text height %d exceeds box height %d", layout.totalHeight, box.H)
		return layout
	}
	layout.fits = true
	return layout
}

func buildTextLines(face xfont.Face, text string, maxWidth int, wrap bool) []textLine {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if !wrap {
		parts := strings.Split(strings.ReplaceAll(text, "\t", "    "), "\n")
		lines := make([]textLine, 0, len(parts))
		for _, part := range parts {
			lines = append(lines, measuredLine(face, part))
		}
		return lines
	}

	paragraphs := strings.Split(text, "\n")
	var lines []textLine
	for _, paragraph := range paragraphs {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			lines = append(lines, textLine{})
			continue
		}
		current := ""
		for _, word := range words {
			if current == "" {
				current = word
				continue
			}
			candidate := current + " " + word
			if measuredLine(face, candidate).widthPx <= maxWidth {
				current = candidate
				continue
			}
			lines = append(lines, measuredLine(face, current))
			current = word
		}
		if current != "" {
			lines = append(lines, measuredLine(face, current))
		}
	}
	return lines
}

func measuredLine(face xfont.Face, s string) textLine {
	if s == "" {
		return textLine{text: s}
	}
	bounds, advance := xfont.BoundString(face, s)
	width := fixedCeil(bounds.Max.X - bounds.Min.X)
	advanceWidth := fixedCeil(advance)
	if advanceWidth > width {
		width = advanceWidth
	}
	return textLine{text: s, widthPx: width}
}

func validateLaidOutBounds(face xfont.Face, node Node, idx int, layout textLayout) *Issue {
	baselineY := firstBaselineY(node.Box, node.VAlign, layout)
	for i, line := range layout.lines {
		baselineX := lineBaselineX(face, node.Box, node.Align, line.text, line.widthPx)
		d := xfont.Drawer{Face: face, Dot: fixed.Point26_6{X: fixed.I(baselineX), Y: fixed.I(baselineY + i*layout.lineAdvance)}}
		bounds, _ := d.BoundString(line.text)
		if fixedFloor(bounds.Min.X) < node.Box.X || fixedCeil(bounds.Max.X) > node.Box.X+node.Box.W || fixedFloor(bounds.Min.Y) < node.Box.Y || fixedCeil(bounds.Max.Y) > node.Box.Y+node.Box.H {
			issue := errorIssue(node.ID, fmt.Sprintf("/nodes/%d/text", idx), "text_overflow", "text glyph bounds would exceed the node box")
			return &issue
		}
	}
	return nil
}

func drawTextLayout(dst *image.Gray, face xfont.Face, node Node, layout textLayout, gray byte) {
	clipRect := image.Rect(node.Box.X, node.Box.Y, node.Box.X+node.Box.W, node.Box.Y+node.Box.H)
	clip := dst.SubImage(clipRect).(*image.Gray)
	d := xfont.Drawer{
		Dst:  clip,
		Src:  image.NewUniform(color.Gray{Y: gray}),
		Face: face,
	}
	baselineY := firstBaselineY(node.Box, node.VAlign, layout)
	for i, line := range layout.lines {
		d.Dot = fixed.Point26_6{X: fixed.I(lineBaselineX(face, node.Box, node.Align, line.text, line.widthPx)), Y: fixed.I(baselineY + i*layout.lineAdvance)}
		d.DrawString(line.text)
	}
}

func firstBaselineY(box Box, valign VAlign, layout textLayout) int {
	slack := box.H - layout.totalHeight
	if slack < 0 {
		slack = 0
	}
	top := box.Y
	switch valign {
	case VAlignMiddle:
		top += slack / 2
	case VAlignBottom:
		top += slack
	}
	return top + layout.ascent
}

func lineBaselineX(face xfont.Face, box Box, align Align, s string, widthPx int) int {
	bounds, _ := xfont.BoundString(face, s)
	leftBearing := fixedFloor(bounds.Min.X)
	if leftBearing > 0 {
		leftBearing = 0
	}
	offset := 0
	slack := box.W - widthPx
	if slack < 0 {
		slack = 0
	}
	switch align {
	case AlignCenter:
		offset = slack / 2
	case AlignRight:
		offset = slack
	}
	baselineX := box.X + offset - leftBearing
	// The placement above anchors on advance width, but ink bounds can
	// extend past the advance (positive left bearing, glyphs overhanging
	// their final advance). Clamp so the ink always stays inside the box;
	// in the common case this shifts nothing.
	inkMin := baselineX + fixedFloor(bounds.Min.X)
	inkMax := baselineX + fixedCeil(bounds.Max.X)
	if inkMax > box.X+box.W {
		baselineX -= inkMax - (box.X + box.W)
	}
	if inkMin < box.X {
		baselineX += box.X - inkMin
	}
	return baselineX
}

func fixedFloor(v fixed.Int26_6) int {
	if v >= 0 {
		return int(v >> 6)
	}
	return -int((-v + 63) >> 6)
}

func fixedCeil(v fixed.Int26_6) int {
	if v >= 0 {
		return int((v + 63) >> 6)
	}
	return -int((-v) >> 6)
}

func noticeIssue(nodeID, path, code, message string) Issue {
	return Issue{Severity: SeverityNotice, Node: nodeID, Path: path, Code: code, Message: message}
}
