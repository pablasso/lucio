package renderer

import (
	"fmt"
	"image"
	"strings"
)

type drawOpRenderMetrics struct {
	FinalBox       Box
	ActualFontSize *int
	Lines          *int
	Image          *ImageFitReport
}

type drawOpRenderResult struct {
	issues  []Issue
	metrics *drawOpRenderMetrics
}

func renderDrawOp(img *image.Gray, op DrawOp, opIndex int, opts RenderOptions) drawOpRenderResult {
	if op.Command.commandCount() != 1 {
		issue := errorIssue(op.Source.ID, op.Source.Path, "bad_draw_op", fmt.Sprintf("draw op %q must have exactly one core command", op.ID))
		if op.ID != op.Source.ID {
			issue.DrawOp = op.ID
		}
		return drawOpRenderResult{issues: []Issue{issue}}
	}

	switch {
	case op.Command.Rect != nil:
		node := nodeFromRectCommand(op.ID, op.Command.Rect)
		issues := renderRect(img, node, opIndex)
		return drawOpRenderResult{issues: sourceMapIssues(issues, op, opIndex)}
	case op.Command.Line != nil:
		node := nodeFromLineCommand(op.ID, op.Command.Line)
		issues := renderLine(img, node, opIndex)
		return drawOpRenderResult{issues: sourceMapIssues(issues, op, opIndex)}
	case op.Command.Text != nil:
		node := nodeFromTextCommand(op.ID, op.Command.Text)
		textResult := renderText(img, node, opIndex)
		metrics := &drawOpRenderMetrics{
			FinalBox: node.Box,
		}
		if textResult.actualFontSize > 0 {
			metrics.ActualFontSize = cloneIntPtr(&textResult.actualFontSize)
		}
		if textResult.lines > 0 {
			metrics.Lines = cloneIntPtr(&textResult.lines)
		}
		return drawOpRenderResult{issues: sourceMapIssues(textResult.issues, op, opIndex), metrics: metrics}
	case op.Command.Image != nil:
		issues, imageReport := renderImageCommand(img, op.Command.Image, opIndex, opts)
		return drawOpRenderResult{issues: sourceMapIssues(issues, op, opIndex), metrics: &drawOpRenderMetrics{FinalBox: op.Command.Image.Box, Image: imageReport}}
	default:
		issue := errorIssue(op.Source.ID, op.Source.Path, "bad_draw_op", fmt.Sprintf("draw op %q has no core command", op.ID))
		if op.ID != op.Source.ID {
			issue.DrawOp = op.ID
		}
		return drawOpRenderResult{issues: []Issue{issue}}
	}
}

func nodeFromRectCommand(id string, cmd *CoreRectCommand) Node {
	return Node{
		ID:          id,
		Type:        NodeTypeRect,
		Box:         cmd.Box,
		Fill:        cmd.Fill,
		Stroke:      cmd.Stroke,
		StrokeWidth: cloneIntPtr(cmd.StrokeWidth),
		Radius:      cloneIntPtr(cmd.Radius),
	}
}

func nodeFromLineCommand(id string, cmd *CoreLineCommand) Node {
	return Node{
		ID:          id,
		Type:        NodeTypeLine,
		Box:         cmd.Box,
		From:        clonePointPtr(cmd.From),
		To:          clonePointPtr(cmd.To),
		Stroke:      cmd.Stroke,
		StrokeWidth: cloneIntPtr(cmd.StrokeWidth),
	}
}

func nodeFromTextCommand(id string, cmd *CoreTextCommand) Node {
	return Node{
		ID:            id,
		Type:          NodeTypeText,
		Box:           cmd.Box,
		Text:          cmd.Text,
		Font:          cloneFontPtr(cmd.Font),
		Align:         cmd.Align,
		VAlign:        cmd.VAlign,
		Color:         cmd.Color,
		Wrap:          cloneBoolPtr(cmd.Wrap),
		MaxLines:      cloneIntPtr(cmd.MaxLines),
		ShrinkToFit:   cloneBoolPtr(cmd.ShrinkToFit),
		ShrinkMinSize: cloneIntPtr(cmd.ShrinkMinSize),
		LineHeight:    cloneFloatPtr(cmd.LineHeight),
	}
}

func sourceMapIssues(issues []Issue, op DrawOp, opIndex int) []Issue {
	if len(issues) == 0 {
		return issues
	}
	mapped := make([]Issue, len(issues))
	for i, issue := range issues {
		issue.Node = op.Source.ID
		issue.Path = sourceMapIssuePath(issue.Path, op, opIndex)
		if op.ID != op.Source.ID {
			issue.DrawOp = op.ID
		}
		mapped[i] = issue
	}
	return mapped
}

func sourceMapIssuePath(path string, op DrawOp, opIndex int) string {
	if op.Source.Path == "" || path == "" {
		return path
	}
	base := fmt.Sprintf("/nodes/%d", opIndex)
	if path == base {
		return op.Source.Path
	}
	if !strings.HasPrefix(path, base+"/") {
		return path
	}
	suffix := strings.TrimPrefix(path, base+"/")
	if mapped := sourceFieldPath(op.Source.FieldPath, suffix); mapped != "" {
		return mapped
	}
	return joinSourcePath(op.Source.Path, suffix)
}

func sourceFieldPath(paths map[string]string, suffix string) string {
	if len(paths) == 0 || suffix == "" {
		return ""
	}
	if p := paths[suffix]; p != "" {
		return p
	}
	dotted := strings.ReplaceAll(suffix, "/", ".")
	if p := paths[dotted]; p != "" {
		return p
	}
	parts := strings.Split(suffix, "/")
	if len(parts) == 0 {
		return ""
	}
	if p := paths[parts[0]]; p != "" {
		if len(parts) == 1 {
			return p
		}
		return joinSourcePath(p, strings.Join(parts[1:], "/"))
	}
	return ""
}

func joinSourcePath(base, suffix string) string {
	if suffix == "" {
		return base
	}
	if base == "" {
		return "/" + suffix
	}
	return base + "/" + suffix
}
