package delivery

import (
	"image"
	"strings"
	"testing"

	"github.com/pablasso/lucio/internal/pv3"
)

func TestPlanSafePartialGeometryNoChanges(t *testing.T) {
	prev := solidGray(255)
	target := cloneTestGray(prev)

	plan, err := PlanSafePartialGeometry(prev, target, SafePartialGeometryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Decision != PartialGeometryNoop || plan.Reason != "no_changes" || len(plan.Strips) != 0 {
		t.Fatalf("plan = %#v, want no-op", plan)
	}
}

func TestPlanSafePartialGeometrySingleRowExpandsToAlignedFullWidthStrip(t *testing.T) {
	prev := solidGray(255)
	target := cloneTestGray(prev)
	setRow(target, 100, 0)

	plan, err := PlanSafePartialGeometry(prev, target, SafePartialGeometryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Decision != PartialGeometryPartial || plan.Reason != "safe_full_width_aligned_strips" {
		t.Fatalf("decision/reason = %q/%q, want partial safe", plan.Decision, plan.Reason)
	}
	if plan.ChangedPortraitRows != 1 || plan.DirtyInternalRows != 1 || plan.DirtyBlocks != 1 || plan.ExpandedRows != 64 {
		t.Fatalf("counts = changed:%d internal:%d blocks:%d expanded:%d", plan.ChangedPortraitRows, plan.DirtyInternalRows, plan.DirtyBlocks, plan.ExpandedRows)
	}
	want := SafePartialStrip{X: 0, Y: 64, W: pv3.InternalRectWidth, H: 64, Screens: []int{0, 1}}
	assertOneStrip(t, plan, want)
}

func TestPlanSafePartialGeometryMapsPortraitBandsToInternalRowOffset(t *testing.T) {
	prev := solidGray(255)
	target := cloneTestGray(prev)
	setRow(target, 10, 0)
	setRow(target, pv3.BandHeight+10, 0)
	setRow(target, 2*pv3.BandHeight+10, 0)
	setRow(target, 3*pv3.BandHeight+10, 0)

	plan, err := PlanSafePartialGeometry(prev, target, SafePartialGeometryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.ChangedPortraitRows != 4 || plan.DirtyInternalRows != 1 || plan.ExpandedRows != 64 {
		t.Fatalf("counts = %#v", plan)
	}
	want := SafePartialStrip{X: 0, Y: 0, W: pv3.InternalRectWidth, H: 64, Screens: []int{0, 1}}
	assertOneStrip(t, plan, want)
}

func TestPlanSafePartialGeometryTooBroadFallsBackFull(t *testing.T) {
	prev := solidGray(255)
	target := cloneTestGray(prev)
	for _, y := range []int{0, 64, 128, 192, 256} {
		setRow(target, y, 0)
	}

	plan, err := PlanSafePartialGeometry(prev, target, SafePartialGeometryOptions{MaxExpandedRows: 256, MaxStrips: 10})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Decision != PartialGeometryFull || !strings.Contains(plan.Reason, "too_broad") {
		t.Fatalf("plan = %#v, want too_broad full fallback", plan)
	}
	if plan.ExpandedRows != 320 || len(plan.Strips) != 0 {
		t.Fatalf("expanded/strips = %d/%d, want 320/0", plan.ExpandedRows, len(plan.Strips))
	}
}

func TestPlanSafePartialGeometryTooFragmentedFallsBackFull(t *testing.T) {
	prev := solidGray(255)
	target := cloneTestGray(prev)
	for _, y := range []int{0, 128, 256} {
		setRow(target, y, 0)
	}

	plan, err := PlanSafePartialGeometry(prev, target, SafePartialGeometryOptions{MaxExpandedRows: 640, MaxStrips: 2})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Decision != PartialGeometryFull || !strings.Contains(plan.Reason, "too_fragmented") {
		t.Fatalf("plan = %#v, want too_fragmented full fallback", plan)
	}
	if plan.ExpandedRows != 192 || len(plan.Strips) != 0 {
		t.Fatalf("expanded/strips = %d/%d, want 192/0", plan.ExpandedRows, len(plan.Strips))
	}
}

func TestPlanSafePartialGeometryRejectsBadGeometry(t *testing.T) {
	prev := image.NewGray(image.Rect(0, 0, 10, 10))
	target := solidGray(255)
	if _, err := PlanSafePartialGeometry(prev, target, SafePartialGeometryOptions{}); err == nil {
		t.Fatal("expected bad previous geometry to fail")
	}

	prev = solidGray(255)
	if _, err := PlanSafePartialGeometry(prev, target, SafePartialGeometryOptions{AlignRows: 63}); err == nil {
		t.Fatal("expected non-dividing alignment to fail")
	}
}

func assertOneStrip(t *testing.T, plan SafePartialPlan, want SafePartialStrip) {
	t.Helper()
	if len(plan.Strips) != 1 {
		t.Fatalf("strips = %#v, want one", plan.Strips)
	}
	got := plan.Strips[0]
	if got.X != want.X || got.Y != want.Y || got.W != want.W || got.H != want.H || len(got.Screens) != len(want.Screens) {
		t.Fatalf("strip = %#v, want %#v", got, want)
	}
	for i := range got.Screens {
		if got.Screens[i] != want.Screens[i] {
			t.Fatalf("strip screens = %#v, want %#v", got.Screens, want.Screens)
		}
	}
}

func solidGray(v byte) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, pv3.PortraitWidth, pv3.PortraitHeight))
	for i := range img.Pix {
		img.Pix[i] = v
	}
	return img
}

func cloneTestGray(src *image.Gray) *image.Gray {
	clone := image.NewGray(src.Bounds())
	for y := 0; y < pv3.PortraitHeight; y++ {
		srcOff := src.PixOffset(0, y)
		dstOff := clone.PixOffset(0, y)
		copy(clone.Pix[dstOff:dstOff+pv3.PortraitWidth], src.Pix[srcOff:srcOff+pv3.PortraitWidth])
	}
	return clone
}

func setRow(img *image.Gray, y int, v byte) {
	off := img.PixOffset(0, y)
	row := img.Pix[off : off+pv3.PortraitWidth]
	for x := range row {
		row[x] = v
	}
}
