package paperboy

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func renderPDFPage(ctx context.Context, pdftocairoPath, pdfPath, outputBase string, renderSize int) error {
	if err := os.Remove(outputBase + ".png"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove previous render: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(
		ctx,
		pdftocairoPath,
		"-png",
		"-singlefile",
		"-f", "1",
		"-l", "1",
		"-scale-to", strconv.Itoa(renderSize),
		pdfPath,
		outputBase,
	)
	cmd.WaitDelay = time.Second
	var outputBuffer boundedBuffer
	cmd.Stdout = &outputBuffer
	cmd.Stderr = &outputBuffer
	err := cmd.Run()
	output := outputBuffer.Bytes()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg != "" {
			return fmt.Errorf("render PDF with pdftocairo: %w: %s", err, msg)
		}
		return fmt.Errorf("render PDF with pdftocairo: %w", err)
	}

	renderedPath := outputBase + ".png"
	stat, err := os.Stat(renderedPath)
	if err != nil {
		return fmt.Errorf("find rendered PNG %s: %w", renderedPath, err)
	}
	if stat.Size() == 0 {
		return fmt.Errorf("rendered PNG is empty: %s", renderedPath)
	}
	return nil
}

type normalizationResult struct {
	SourceWidth  int
	SourceHeight int
	Width        int
	Height       int
}

func normalizeImageFile(srcPath, outPath string) (normalizationResult, error) {
	f, err := os.Open(srcPath)
	if err != nil {
		return normalizationResult{}, fmt.Errorf("open rendered PNG %s: %w", srcPath, err)
	}
	defer f.Close()

	if stat, err := f.Stat(); err != nil {
		return normalizationResult{}, err
	} else if stat.Size() > 64<<20 {
		return normalizationResult{}, fmt.Errorf("rendered PNG exceeds 64 MiB")
	}
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		return normalizationResult{}, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 5120 || cfg.Height > 5120 {
		return normalizationResult{}, fmt.Errorf("rendered PNG exceeds 5120 × 5120")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return normalizationResult{}, err
	}
	src, err := png.Decode(io.LimitReader(f, 64<<20))
	if err != nil {
		return normalizationResult{}, fmt.Errorf("decode rendered PNG %s: %w", srcPath, err)
	}
	bounds := src.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return normalizationResult{}, fmt.Errorf("rendered PNG has invalid dimensions: %dx%d", bounds.Dx(), bounds.Dy())
	}

	dst := normalizeImage(src, canvasWidth, canvasHeight)
	if err := writePNGAtomic(outPath, dst); err != nil {
		return normalizationResult{}, err
	}
	return normalizationResult{SourceWidth: bounds.Dx(), SourceHeight: bounds.Dy(), Width: canvasWidth, Height: canvasHeight}, nil
}

func normalizeImage(src image.Image, dstWidth, dstHeight int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, dstWidth, dstHeight))
	bounds := src.Bounds()
	srcWidth := bounds.Dx()
	srcHeight := bounds.Dy()
	if srcWidth <= 0 || srcHeight <= 0 || dstWidth <= 0 || dstHeight <= 0 {
		return dst
	}

	// Match CSS object-contain: fit the entire page inside the destination and
	// center it, padding the shorter dimension with white. The front pages are
	// taller (narrower) than the 1440x2560 canvas, so this preserves the
	// masthead and bottom of every rotation paper; center-cropping clipped the
	// nameplate on the narrower pages. Use area resampling so the
	// high-resolution Poppler render is averaged down instead of
	// nearest-neighbor sampled.
	scale := math.Min(float64(dstWidth)/float64(srcWidth), float64(dstHeight)/float64(srcHeight))
	drawWidth := float64(srcWidth) * scale
	drawHeight := float64(srcHeight) * scale
	dx := (float64(dstWidth) - drawWidth) / 2
	dy := (float64(dstHeight) - drawHeight) / 2

	for y := 0; y < dstHeight; y++ {
		for x := 0; x < dstWidth; x++ {
			r, g, b := areaSampleObjectContain(src, x, y, scale, dx, dy)
			i := dst.PixOffset(x, y)
			dst.Pix[i] = clampByte(r)
			dst.Pix[i+1] = clampByte(g)
			dst.Pix[i+2] = clampByte(b)
			dst.Pix[i+3] = 255
		}
	}
	return dst
}

func areaSampleObjectContain(src image.Image, x, y int, scale, dx, dy float64) (float64, float64, float64) {
	bounds := src.Bounds()
	srcWidth := bounds.Dx()
	srcHeight := bounds.Dy()

	srcX0 := (float64(x) - dx) / scale
	srcX1 := (float64(x+1) - dx) / scale
	srcY0 := (float64(y) - dy) / scale
	srcY1 := (float64(y+1) - dy) / scale
	clippedX0 := math.Max(0, srcX0)
	clippedX1 := math.Min(float64(srcWidth), srcX1)
	clippedY0 := math.Max(0, srcY0)
	clippedY1 := math.Min(float64(srcHeight), srcY1)

	if clippedX0 >= clippedX1 || clippedY0 >= clippedY1 {
		return 255, 255, 255
	}

	ix0 := int(math.Floor(clippedX0))
	ix1 := int(math.Ceil(clippedX1))
	iy0 := int(math.Floor(clippedY0))
	iy1 := int(math.Ceil(clippedY1))

	var r, g, b, total float64
	for sy := iy0; sy < iy1; sy++ {
		if sy < 0 || sy >= srcHeight {
			continue
		}
		yOverlap := math.Min(clippedY1, float64(sy+1)) - math.Max(clippedY0, float64(sy))
		if yOverlap <= 0 {
			continue
		}
		for sx := ix0; sx < ix1; sx++ {
			if sx < 0 || sx >= srcWidth {
				continue
			}
			xOverlap := math.Min(clippedX1, float64(sx+1)) - math.Max(clippedX0, float64(sx))
			if xOverlap <= 0 {
				continue
			}
			weight := xOverlap * yOverlap
			pr, pg, pb := pixelOverWhite(src, bounds.Min.X+sx, bounds.Min.Y+sy)
			r += pr * weight
			g += pg * weight
			b += pb * weight
			total += weight
		}
	}

	if total <= 0 {
		return 255, 255, 255
	}
	return r / total, g / total, b / total
}

func pixelOverWhite(src image.Image, x, y int) (float64, float64, float64) {
	r, g, b, a := src.At(x, y).RGBA()
	const max16 = 0xffff
	outR := r + max16 - a
	outG := g + max16 - a
	outB := b + max16 - a
	if outR > max16 {
		outR = max16
	}
	if outG > max16 {
		outG = max16
	}
	if outB > max16 {
		outB = max16
	}
	return float64(outR) / 257, float64(outG) / 257, float64(outB) / 257
}

func writePNGAtomic(outPath string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(outPath), "."+filepath.Base(outPath)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary output PNG: %w", err)
	}
	tmpPath := tmp.Name()

	encodeErr := png.Encode(tmp, img)
	closeErr := tmp.Close()
	if encodeErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("encode output PNG: %w", encodeErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close output PNG: %w", closeErr)
	}
	if err := os.Rename(tmpPath, outPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename output PNG: %w", err)
	}
	return nil
}

func clampByte(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(math.Round(v))
}
