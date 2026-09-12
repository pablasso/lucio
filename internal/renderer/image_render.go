package renderer

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"io"
	"math"
	"os"
	"strings"

	xdraw "golang.org/x/image/draw"
)

func renderImageCommand(dst *image.Gray, cmd *CoreImageCommand, idx int, opts RenderOptions) ([]Issue, *ImageFitReport) {
	resolved, err := ResolveAssetPath(cmd.Src, opts)
	if err != nil {
		return []Issue{errorIssue("", fmt.Sprintf("/nodes/%d/src", idx), "asset_resolve_failed", err.Error())}, nil
	}
	f, err := os.Open(resolved)
	if err != nil {
		return []Issue{errorIssue("", fmt.Sprintf("/nodes/%d/src", idx), "asset_open_failed", err.Error())}, nil
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, MaxAssetBytes+1))
	if err != nil || len(data) > MaxAssetBytes {
		return []Issue{errorIssue("", fmt.Sprintf("/nodes/%d/src", idx), "asset_limit", "asset exceeds read/size limit")}, nil
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || !validImageSize(cfg.Width, cfg.Height) {
		return []Issue{errorIssue("", fmt.Sprintf("/nodes/%d/src", idx), "asset_limit", "invalid or oversized image dimensions")}, nil
	}
	src, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return []Issue{errorIssue("", fmt.Sprintf("/nodes/%d/src", idx), "asset_decode_failed", fmt.Sprintf("decode image %s: %v", resolved, err))}, nil
	}
	format = strings.ToLower(format)
	if format != "png" && format != "jpeg" {
		return []Issue{errorIssue("", fmt.Sprintf("/nodes/%d/src", idx), "asset_decode_failed", fmt.Sprintf("unsupported image format %q; use PNG or JPEG", format))}, nil
	}

	bounds := src.Bounds()
	sw, sh := bounds.Dx(), bounds.Dy()
	bw, bh := cmd.Box.W, cmd.Box.H
	fit := cmd.Fit
	if fit == "" {
		fit = ImageFitExact
	}
	background := cmd.Background
	if background == "" {
		background = DefaultCanvasBackground
	}
	bgRGB, err := HexColorToRGB(background)
	if err != nil {
		return []Issue{errorIssue("", fmt.Sprintf("/nodes/%d/background", idx), "bad_color", err.Error())}, nil
	}
	bgGray := rgbToGray(bgRGB.R, bgRGB.G, bgRGB.B)

	report := &ImageFitReport{
		SourcePath:   cmd.Src,
		SourceWidth:  sw,
		SourceHeight: sh,
		Fit:          string(fit),
		Background:   background,
	}

	srcOpaque := compositeImageOverBackground(src, bgRGB)
	switch fit {
	case ImageFitExact:
		if sw != bw || sh != bh {
			return []Issue{errorIssue("", fmt.Sprintf("/nodes/%d/src", idx), "asset_size_mismatch", fmt.Sprintf("image dimensions are %dx%d, but node box is %dx%d", sw, sh, bw, bh))}, report
		}
		rect := cmd.Box
		report.FinalDrawRect = &rect
		report.ScaledSize = &Size{W: bw, H: bh}
		copyNRGBAToGray(dst, srcOpaque, image.Rect(0, 0, bw, bh), cmd.Box.X, cmd.Box.Y)
	case ImageFitContain:
		dw, dh := containSize(sw, sh, bw, bh)
		dx := clampInt(roundFloat(float64(bw-dw)*cmd.Position.X), 0, bw-dw)
		dy := clampInt(roundFloat(float64(bh-dh)*cmd.Position.Y), 0, bh-dh)
		fillBoxGray(dst, cmd.Box, bgGray)
		scaled := scaleNRGBA(srcOpaque, dw, dh)
		copyNRGBAToGray(dst, scaled, image.Rect(0, 0, dw, dh), cmd.Box.X+dx, cmd.Box.Y+dy)
		finalRect := Box{X: cmd.Box.X + dx, Y: cmd.Box.Y + dy, W: dw, H: dh}
		report.FinalDrawRect = &finalRect
		report.ScaledSize = &Size{W: dw, H: dh}
	case ImageFitCover:
		dw, dh := coverSize(sw, sh, bw, bh)
		if !validImageSize(dw, dh) {
			return []Issue{errorIssue("", fmt.Sprintf("/nodes/%d/fit", idx), "asset_limit", "cover would allocate an oversized intermediate image; use contain")}, report
		}
		sx := clampInt(roundFloat(float64(dw-bw)*cmd.Position.X), 0, dw-bw)
		sy := clampInt(roundFloat(float64(dh-bh)*cmd.Position.Y), 0, dh-bh)
		scaled := scaleNRGBA(srcOpaque, dw, dh)
		copyNRGBAToGray(dst, scaled, image.Rect(sx, sy, sx+bw, sy+bh), cmd.Box.X, cmd.Box.Y)
		finalRect := cmd.Box
		scaledCrop := Box{X: sx, Y: sy, W: bw, H: bh}
		sourceCrop := Box{X: roundDiv(sx*sw, dw), Y: roundDiv(sy*sh, dh), W: roundDiv(bw*sw, dw), H: roundDiv(bh*sh, dh)}
		report.FinalDrawRect = &finalRect
		report.ScaledSize = &Size{W: dw, H: dh}
		report.ScaledCropRect = &scaledCrop
		report.SourceCropRect = &sourceCrop
	case ImageFitStretch:
		scaled := scaleNRGBA(srcOpaque, bw, bh)
		copyNRGBAToGray(dst, scaled, image.Rect(0, 0, bw, bh), cmd.Box.X, cmd.Box.Y)
		finalRect := cmd.Box
		report.FinalDrawRect = &finalRect
		report.ScaledSize = &Size{W: bw, H: bh}
	default:
		return []Issue{errorIssue("", fmt.Sprintf("/nodes/%d/fit", idx), "bad_enum", "fit must be exact, contain, cover, or stretch")}, report
	}
	return nil, report
}

type rgb8 struct {
	R int
	G int
	B int
}

func HexColorToRGB(s string) (rgb8, error) {
	if !isHexColor(s) {
		return rgb8{}, fmt.Errorf("must be a #rrggbb color")
	}
	return rgb8{R: hexPair(s[1], s[2]), G: hexPair(s[3], s[4]), B: hexPair(s[5], s[6])}, nil
}

func compositeImageOverBackground(src image.Image, bg rgb8) *image.NRGBA {
	bounds := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			r, g, b, a := colorToNRGBA8(src.At(bounds.Min.X+x, bounds.Min.Y+y))
			dst.SetNRGBA(x, y, color.NRGBA{R: uint8(composite8(r, a, bg.R)), G: uint8(composite8(g, a, bg.G)), B: uint8(composite8(b, a, bg.B)), A: 255})
		}
	}
	return dst
}

func colorToNRGBA8(c color.Color) (r, g, b, a int) {
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	return int(n.R), int(n.G), int(n.B), int(n.A)
}

func composite8(src, alpha, bg int) int {
	return (src*alpha + bg*(255-alpha) + 127) / 255
}

func containSize(sw, sh, bw, bh int) (int, int) {
	if bw*sh <= bh*sw {
		dw := bw
		dh := roundDiv(sh*bw, sw)
		return clampInt(dw, 1, bw), clampInt(dh, 1, bh)
	}
	dh := bh
	dw := roundDiv(sw*bh, sh)
	return clampInt(dw, 1, bw), clampInt(dh, 1, bh)
}

func coverSize(sw, sh, bw, bh int) (int, int) {
	if bw*sh >= bh*sw {
		return bw, ceilDiv(sh*bw, sw)
	}
	return ceilDiv(sw*bh, sh), bh
}

func scaleNRGBA(src *image.NRGBA, w, h int) *image.NRGBA {
	if src.Bounds().Dx() == w && src.Bounds().Dy() == h {
		return src
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	return dst
}

func copyNRGBAToGray(dst *image.Gray, src *image.NRGBA, srcRect image.Rectangle, dstX, dstY int) {
	for y := 0; y < srcRect.Dy(); y++ {
		for x := 0; x < srcRect.Dx(); x++ {
			c := src.NRGBAAt(srcRect.Min.X+x, srcRect.Min.Y+y)
			dst.Pix[dst.PixOffset(dstX+x, dstY+y)] = rgbToGray(int(c.R), int(c.G), int(c.B))
		}
	}
}

func fillBoxGray(img *image.Gray, box Box, gray byte) {
	for y := 0; y < box.H; y++ {
		row := img.Pix[img.PixOffset(box.X, box.Y+y) : img.PixOffset(box.X, box.Y+y)+box.W]
		for x := range row {
			row[x] = gray
		}
	}
}

func rgbToGray(r, g, b int) byte {
	return byte((299*r + 587*g + 114*b + 500) / 1000)
}

func roundDiv(n, d int) int {
	if d <= 0 {
		return 0
	}
	return (n + d/2) / d
}

func roundFloat(v float64) int {
	return int(math.Floor(v + 0.5))
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
