package renderer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

type RenderOptions struct {
	// AssetRoot is the base directory for relative image assets. When empty,
	// relative paths resolve against ScenePath's directory, or cwd for stdin.
	AssetRoot string
	ScenePath string

	// ReportDetail includes source-mapped display-list details in the report.
	// It is off by default to keep simple reports concise.
	ReportDetail bool
}

type RenderResult struct {
	Scene  AuthorScene
	Image  *image.Gray
	PNG    []byte
	Report Report
}

func RenderSceneJSON(data []byte, options ...RenderOptions) RenderResult {
	opts := RenderOptions{}
	if len(options) > 0 {
		opts = options[0]
	}
	compiled, report := compileSceneJSON(data, opts.ReportDetail)
	result := RenderResult{Scene: compiled.Author, Report: report}
	if HasErrors(report.Issues) {
		return result
	}

	img := image.NewGray(image.Rect(0, 0, CanvasWidth, CanvasHeight))
	result.Image = img
	bg, err := HexColorToGray(compiled.Author.Canvas.Background)
	if err != nil {
		report.Issues = append(report.Issues, errorIssue("", "/canvas/background", "bad_color", err.Error()))
		report.OK = false
		result.Report = report
		return result
	}
	fillCanvas(img, bg)

	metrics := map[string]drawOpRenderMetrics{}
	for i, op := range compiled.DisplayList.Ops {
		drawResult := renderDrawOp(img, op, i, opts)
		report.Issues = append(report.Issues, drawResult.issues...)
		if drawResult.metrics != nil {
			metrics[op.ID] = *drawResult.metrics
			if op.ID == op.Source.ID && op.Command.Text != nil && drawResult.metrics.ActualFontSize != nil && drawResult.metrics.Lines != nil {
				setTextNodeRenderInfoByID(&report, op.Source.ID, *drawResult.metrics.ActualFontSize, *drawResult.metrics.Lines)
			}
		}
	}
	if opts.ReportDetail || compiledNeedsRichNodeReport(compiled) {
		report.DrawOps = buildDrawOpReports(compiled.DisplayList, metrics)
	}
	report.OK = !HasErrors(report.Issues)
	if !report.OK {
		result.Report = report
		return result
	}

	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		report.Issues = append(report.Issues, errorIssue("", "/output", "png_encode_failed", err.Error()))
		report.OK = false
		result.Report = report
		return result
	}
	pngBytes := pngBuf.Bytes()
	sum := sha256.Sum256(pngBytes)
	report.Output.SHA256 = hex.EncodeToString(sum[:])
	report.OK = true
	result.PNG = pngBytes
	result.Report = report
	return result
}

func HexColorToGray(s string) (byte, error) {
	if !isHexColor(s) {
		return 0, fmt.Errorf("must be a #rrggbb color")
	}
	r := hexPair(s[1], s[2])
	g := hexPair(s[3], s[4])
	b := hexPair(s[5], s[6])
	return byte((299*r + 587*g + 114*b + 500) / 1000), nil
}

func hexPair(a, b byte) int {
	return hexNibble(a)*16 + hexNibble(b)
}

func hexNibble(b byte) int {
	switch {
	case b >= '0' && b <= '9':
		return int(b - '0')
	case b >= 'a' && b <= 'f':
		return int(b-'a') + 10
	case b >= 'A' && b <= 'F':
		return int(b-'A') + 10
	default:
		return 0
	}
}

func fillCanvas(img *image.Gray, gray byte) {
	for y := 0; y < CanvasHeight; y++ {
		row := img.Pix[img.PixOffset(0, y) : img.PixOffset(0, y)+CanvasWidth]
		for x := range row {
			row[x] = gray
		}
	}
}

func ResolveAssetPath(src string, opts RenderOptions) (string, error) {
	if src == "" {
		return "", fmt.Errorf("asset src must not be empty")
	}
	if filepath.IsAbs(src) {
		return filepath.Clean(src), nil
	}
	base := opts.AssetRoot
	if base == "" {
		if opts.ScenePath != "" && opts.ScenePath != "-" {
			base = filepath.Dir(opts.ScenePath)
		} else {
			cwd, err := os.Getwd()
			if err != nil {
				return "", err
			}
			base = cwd
		}
	}
	return filepath.Clean(filepath.Join(base, src)), nil
}

func renderRect(img *image.Gray, node Node, idx int) []Issue {
	var issues []Issue
	radius := 0
	if node.Radius != nil {
		radius = *node.Radius
	}
	if node.Fill != "" {
		gray, err := HexColorToGray(node.Fill)
		if err != nil {
			issues = append(issues, errorIssue(node.ID, fmt.Sprintf("/nodes/%d/fill", idx), "bad_color", err.Error()))
		} else {
			fillRoundedRect(img, node.Box, gray, radius)
		}
	}
	if node.Stroke != "" {
		strokeWidth := 1
		if node.StrokeWidth != nil {
			strokeWidth = *node.StrokeWidth
		}
		if strokeWidth > 0 {
			gray, err := HexColorToGray(node.Stroke)
			if err != nil {
				issues = append(issues, errorIssue(node.ID, fmt.Sprintf("/nodes/%d/stroke", idx), "bad_color", err.Error()))
			} else {
				strokeRoundedRect(img, node.Box, gray, strokeWidth, radius)
			}
		}
	}
	return issues
}

func fillRoundedRect(img *image.Gray, box Box, gray byte, radius int) {
	for y := 0; y < box.H; y++ {
		for x := 0; x < box.W; x++ {
			if inRoundedRectLocal(x, y, box.W, box.H, radius) {
				img.Pix[img.PixOffset(box.X+x, box.Y+y)] = gray
			}
		}
	}
}

func strokeRoundedRect(img *image.Gray, box Box, gray byte, strokeWidth, radius int) {
	inner := Box{X: strokeWidth, Y: strokeWidth, W: box.W - 2*strokeWidth, H: box.H - 2*strokeWidth}
	innerRadius := radius - strokeWidth
	if innerRadius < 0 {
		innerRadius = 0
	}
	for y := 0; y < box.H; y++ {
		for x := 0; x < box.W; x++ {
			if !inRoundedRectLocal(x, y, box.W, box.H, radius) {
				continue
			}
			insideInner := inner.W > 0 && inner.H > 0 && x >= inner.X && y >= inner.Y && x < inner.X+inner.W && y < inner.Y+inner.H && inRoundedRectLocal(x-inner.X, y-inner.Y, inner.W, inner.H, innerRadius)
			if !insideInner {
				img.Pix[img.PixOffset(box.X+x, box.Y+y)] = gray
			}
		}
	}
}

func inRoundedRectLocal(x, y, w, h, radius int) bool {
	if x < 0 || y < 0 || x >= w || y >= h {
		return false
	}
	if radius <= 0 {
		return true
	}
	if radius*2 > w {
		radius = w / 2
	}
	if radius*2 > h {
		radius = h / 2
	}
	if radius <= 0 {
		return true
	}
	if (x >= radius && x < w-radius) || (y >= radius && y < h-radius) {
		return true
	}

	pcx2 := 2*x + 1
	pcy2 := 2*y + 1
	cx2 := 2 * radius
	cy2 := 2 * radius
	if x >= w-radius {
		cx2 = 2 * (w - radius)
	}
	if y >= h-radius {
		cy2 = 2 * (h - radius)
	}
	dx := pcx2 - cx2
	dy := pcy2 - cy2
	r2 := 2 * radius
	return dx*dx+dy*dy <= r2*r2
}

func renderLine(img *image.Gray, node Node, idx int) []Issue {
	var issues []Issue
	if node.From == nil || node.To == nil {
		return issues
	}
	stroke := node.Stroke
	if stroke == "" {
		stroke = DefaultTextColor
	}
	gray, err := HexColorToGray(stroke)
	if err != nil {
		return append(issues, errorIssue(node.ID, fmt.Sprintf("/nodes/%d/stroke", idx), "bad_color", err.Error()))
	}
	strokeWidth := 1
	if node.StrokeWidth != nil {
		strokeWidth = *node.StrokeWidth
	}
	if strokeWidth <= 0 {
		return append(issues, errorIssue(node.ID, fmt.Sprintf("/nodes/%d/stroke_width", idx), "out_of_range", "stroke_width must be positive for line nodes"))
	}

	x0 := node.Box.X + node.From.X
	y0 := node.Box.Y + node.From.Y
	x1 := node.Box.X + node.To.X
	y1 := node.Box.Y + node.To.Y

	if y0 == y1 {
		if issue := validateHorizontalLineStroke(node, idx, y0, strokeWidth); issue != nil {
			return append(issues, *issue)
		}
		drawHorizontalLine(img, x0, x1, y0, strokeWidth, gray)
		return issues
	}
	if x0 == x1 {
		if issue := validateVerticalLineStroke(node, idx, x0, strokeWidth); issue != nil {
			return append(issues, *issue)
		}
		drawVerticalLine(img, x0, y0, y1, strokeWidth, gray)
		return issues
	}
	if issue := validateGeneralLineStroke(node, idx, x0, y0, x1, y1, strokeWidth); issue != nil {
		return append(issues, *issue)
	}
	drawGeneralLine(img, node.Box, x0, y0, x1, y1, strokeWidth, gray)
	return issues
}

func validateHorizontalLineStroke(node Node, idx, y, strokeWidth int) *Issue {
	lo := y - (strokeWidth-1)/2
	hi := y + strokeWidth/2
	if lo < node.Box.Y || hi >= node.Box.Y+node.Box.H {
		issue := errorIssue(node.ID, fmt.Sprintf("/nodes/%d", idx), "line_stroke_out_of_bounds", "line stroke must fit inside the node box")
		return &issue
	}
	return nil
}

func validateVerticalLineStroke(node Node, idx, x, strokeWidth int) *Issue {
	lo := x - (strokeWidth-1)/2
	hi := x + strokeWidth/2
	if lo < node.Box.X || hi >= node.Box.X+node.Box.W {
		issue := errorIssue(node.ID, fmt.Sprintf("/nodes/%d", idx), "line_stroke_out_of_bounds", "line stroke must fit inside the node box")
		return &issue
	}
	return nil
}

func validateGeneralLineStroke(node Node, idx, x0, y0, x1, y1, strokeWidth int) *Issue {
	expand := int(math.Ceil(float64(strokeWidth) / 2.0))
	minX := minInt(x0, x1) - expand
	maxX := maxInt(x0, x1) + expand
	minY := minInt(y0, y1) - expand
	maxY := maxInt(y0, y1) + expand
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			if !lineCoversPixel(x, y, x0, y0, x1, y1, strokeWidth) {
				continue
			}
			if x < node.Box.X || y < node.Box.Y || x >= node.Box.X+node.Box.W || y >= node.Box.Y+node.Box.H {
				issue := errorIssue(node.ID, fmt.Sprintf("/nodes/%d", idx), "line_stroke_out_of_bounds", "line stroke must fit inside the node box")
				return &issue
			}
		}
	}
	return nil
}

func drawHorizontalLine(img *image.Gray, x0, x1, y, strokeWidth int, gray byte) {
	if x1 < x0 {
		x0, x1 = x1, x0
	}
	lo := y - (strokeWidth-1)/2
	hi := y + strokeWidth/2
	for yy := lo; yy <= hi; yy++ {
		for x := x0; x <= x1; x++ {
			img.Pix[img.PixOffset(x, yy)] = gray
		}
	}
}

func drawVerticalLine(img *image.Gray, x, y0, y1, strokeWidth int, gray byte) {
	if y1 < y0 {
		y0, y1 = y1, y0
	}
	lo := x - (strokeWidth-1)/2
	hi := x + strokeWidth/2
	for xx := lo; xx <= hi; xx++ {
		for y := y0; y <= y1; y++ {
			img.Pix[img.PixOffset(xx, y)] = gray
		}
	}
}

func drawGeneralLine(img *image.Gray, box Box, x0, y0, x1, y1, strokeWidth int, gray byte) {
	for y := box.Y; y < box.Y+box.H; y++ {
		for x := box.X; x < box.X+box.W; x++ {
			if lineCoversPixel(x, y, x0, y0, x1, y1, strokeWidth) {
				img.Pix[img.PixOffset(x, y)] = gray
			}
		}
	}
}

func lineCoversPixel(x, y, x0, y0, x1, y1, strokeWidth int) bool {
	dx := float64(x1 - x0)
	dy := float64(y1 - y0)
	px := float64(x - x0)
	py := float64(y - y0)
	length2 := dx*dx + dy*dy
	radius := float64(strokeWidth) / 2.0
	if length2 == 0 {
		return math.Hypot(px, py) <= radius
	}
	t := (px*dx + py*dy) / length2
	if t < 0 || t > 1 {
		return false
	}
	closestX := float64(x0) + t*dx
	closestY := float64(y0) + t*dy
	return math.Hypot(float64(x)-closestX, float64(y)-closestY) <= radius
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
