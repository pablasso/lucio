package delivery

import (
	"bytes"
	"fmt"
	"image"

	"github.com/pablasso/lucio/internal/pv3"
)

const (
	DefaultSafePartialAlignRows       = 64
	DefaultSafePartialMaxExpandedRows = 256
	DefaultSafePartialMaxStrips       = 4
)

type PartialGeometryDecision string

const (
	PartialGeometryNoop    PartialGeometryDecision = "noop"
	PartialGeometryPartial PartialGeometryDecision = "partial"
	PartialGeometryFull    PartialGeometryDecision = "full"
)

type SafePartialGeometryOptions struct {
	AlignRows       int
	MaxExpandedRows int
	MaxStrips       int
}

type SafePartialPlan struct {
	Decision            PartialGeometryDecision `json:"decision"`
	Reason              string                  `json:"reason"`
	Strips              []SafePartialStrip      `json:"strips,omitempty"`
	ChangedPortraitRows int                     `json:"changed_portrait_rows"`
	DirtyInternalRows   int                     `json:"dirty_internal_rows"`
	DirtyBlocks         int                     `json:"dirty_blocks"`
	ExpandedRows        int                     `json:"expanded_rows"`
	AlignmentRows       int                     `json:"alignment_rows"`
	MaxExpandedRows     int                     `json:"max_expanded_rows"`
	MaxStrips           int                     `json:"max_strips"`
}

type SafePartialStrip struct {
	X       int   `json:"x"`
	Y       int   `json:"y"`
	W       int   `json:"w"`
	H       int   `json:"h"`
	Screens []int `json:"screens"`
}

func DefaultSafePartialGeometryOptions() SafePartialGeometryOptions {
	return SafePartialGeometryOptions{
		AlignRows:       DefaultSafePartialAlignRows,
		MaxExpandedRows: DefaultSafePartialMaxExpandedRows,
		MaxStrips:       DefaultSafePartialMaxStrips,
	}
}

func NewSafePartialFullPlan(reason string, opts SafePartialGeometryOptions) SafePartialPlan {
	norm, err := normalizeSafePartialGeometryOptions(opts)
	if err != nil {
		norm = DefaultSafePartialGeometryOptions()
	}
	return SafePartialPlan{
		Decision:        PartialGeometryFull,
		Reason:          reason,
		AlignmentRows:   norm.AlignRows,
		MaxExpandedRows: norm.MaxExpandedRows,
		MaxStrips:       norm.MaxStrips,
	}
}

func (p SafePartialPlan) CanPartial() bool { return p.Decision == PartialGeometryPartial }

func PlanSafePartialGeometry(previous, target *image.Gray, opts SafePartialGeometryOptions) (SafePartialPlan, error) {
	norm, err := normalizeSafePartialGeometryOptions(opts)
	if err != nil {
		return SafePartialPlan{}, err
	}
	plan := SafePartialPlan{
		AlignmentRows:   norm.AlignRows,
		MaxExpandedRows: norm.MaxExpandedRows,
		MaxStrips:       norm.MaxStrips,
	}
	if err := validateSafePartialGray("previous", previous); err != nil {
		return SafePartialPlan{}, err
	}
	if err := validateSafePartialGray("target", target); err != nil {
		return SafePartialPlan{}, err
	}

	dirtyInternalRows := make([]bool, pv3.InternalRectHeight)
	for y := 0; y < pv3.PortraitHeight; y++ {
		if !bytes.Equal(grayRow(previous, y), grayRow(target, y)) {
			plan.ChangedPortraitRows++
			dirtyInternalRows[y%pv3.BandHeight] = true
		}
	}
	if plan.ChangedPortraitRows == 0 {
		plan.Decision = PartialGeometryNoop
		plan.Reason = "no_changes"
		return plan, nil
	}

	blocks := make([]bool, pv3.InternalRectHeight/norm.AlignRows)
	for internalY, dirty := range dirtyInternalRows {
		if !dirty {
			continue
		}
		plan.DirtyInternalRows++
		blocks[internalY/norm.AlignRows] = true
	}
	for block, dirty := range blocks {
		if !dirty {
			continue
		}
		plan.DirtyBlocks++
		plan.ExpandedRows += norm.AlignRows
		plan.Strips = append(plan.Strips, SafePartialStrip{
			X:       0,
			Y:       block * norm.AlignRows,
			W:       pv3.InternalRectWidth,
			H:       norm.AlignRows,
			Screens: []int{0, 1},
		})
	}

	if plan.ExpandedRows > norm.MaxExpandedRows {
		plan.Decision = PartialGeometryFull
		plan.Reason = fmt.Sprintf("too_broad_expanded_rows_%d_gt_%d", plan.ExpandedRows, norm.MaxExpandedRows)
		plan.Strips = nil
		return plan, nil
	}
	if len(plan.Strips) > norm.MaxStrips {
		plan.Decision = PartialGeometryFull
		plan.Reason = fmt.Sprintf("too_fragmented_strips_%d_gt_%d", len(plan.Strips), norm.MaxStrips)
		plan.Strips = nil
		return plan, nil
	}

	plan.Decision = PartialGeometryPartial
	plan.Reason = "safe_full_width_aligned_strips"
	return plan, nil
}

func normalizeSafePartialGeometryOptions(opts SafePartialGeometryOptions) (SafePartialGeometryOptions, error) {
	if opts.AlignRows == 0 {
		opts.AlignRows = DefaultSafePartialAlignRows
	}
	if opts.MaxExpandedRows == 0 {
		opts.MaxExpandedRows = DefaultSafePartialMaxExpandedRows
	}
	if opts.MaxStrips == 0 {
		opts.MaxStrips = DefaultSafePartialMaxStrips
	}
	if opts.AlignRows <= 0 {
		return SafePartialGeometryOptions{}, fmt.Errorf("partial geometry align rows must be positive")
	}
	if pv3.InternalRectHeight%opts.AlignRows != 0 {
		return SafePartialGeometryOptions{}, fmt.Errorf("partial geometry align rows %d must divide internal height %d", opts.AlignRows, pv3.InternalRectHeight)
	}
	if opts.MaxExpandedRows <= 0 {
		return SafePartialGeometryOptions{}, fmt.Errorf("partial geometry max expanded rows must be positive")
	}
	if opts.MaxStrips <= 0 {
		return SafePartialGeometryOptions{}, fmt.Errorf("partial geometry max strips must be positive")
	}
	return opts, nil
}

func validateSafePartialGray(name string, img *image.Gray) error {
	if img == nil {
		return fmt.Errorf("%s framebuffer is nil", name)
	}
	b := img.Bounds()
	if b.Min.X != 0 || b.Min.Y != 0 || b.Dx() != pv3.PortraitWidth || b.Dy() != pv3.PortraitHeight {
		return fmt.Errorf("%s framebuffer must have bounds (0,0)-(%d,%d), got %v", name, pv3.PortraitWidth, pv3.PortraitHeight, b)
	}
	if img.Stride < pv3.PortraitWidth || len(img.Pix) < pv3.PortraitWidth || img.Stride > (len(img.Pix)-pv3.PortraitWidth)/(pv3.PortraitHeight-1) {
		return fmt.Errorf("%s framebuffer has invalid pixel storage or stride", name)
	}
	return nil
}

func grayRow(img *image.Gray, y int) []byte {
	off := img.PixOffset(0, y)
	return img.Pix[off : off+pv3.PortraitWidth]
}
