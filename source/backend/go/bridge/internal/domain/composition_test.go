package domain

import "testing"

func allLive(SourceRef) bool { return true }

func stageRoom(t *testing.T, layout LayoutMode) (*Room, *Participant, *Participant) {
	t.Helper()
	r := newTestRoom(t, 0)
	r.Settings.Layout = layout
	a, _ := r.Join("Ana", "", true, now)
	b, _ := r.Join("Beto", "", true, now)
	for _, p := range []*Participant{a, b} {
		r.SetPublished(p.ID, SourceCamera, true)
		r.SetPublished(p.ID, SourceMicrophone, true)
	}
	return r, a, b
}

func TestEmptyStageShowsTitle(t *testing.T) {
	plan := PlanComposition(newTestRoom(t, 0), DefaultOutput, allLive)
	if len(plan.Tiles) != 0 || plan.Title != "Demo" || len(plan.Audio) != 0 {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestGridOfCameras(t *testing.T) {
	r, _, _ := stageRoom(t, LayoutAuto)
	plan := PlanComposition(r, DefaultOutput, allLive)
	if len(plan.Tiles) != 2 || plan.Tiles[0].Label != "Ana" || plan.Tiles[1].Label != "Beto" {
		t.Fatalf("tiles = %+v", plan.Tiles)
	}
	a, b := plan.Tiles[0].Rect, plan.Tiles[1].Rect
	if a.Y != b.Y || a.X >= b.X || a.W != b.W || a.W*9 != a.H*16 && abs(a.W*9-a.H*16) > 32 {
		t.Fatalf("expected two 16:9 tiles side by side, got %+v %+v", a, b)
	}
	for _, tile := range plan.Tiles {
		r := tile.Rect
		if r.X%2 != 0 || r.Y%2 != 0 || r.W%2 != 0 || r.H%2 != 0 || r.X+r.W > 1280 || r.Y+r.H > 720 {
			t.Fatalf("tile out of canvas or odd: %+v", r)
		}
	}
	if len(plan.Audio) != 2 || len(plan.VideoSources()) != 2 {
		t.Fatalf("audio %v video %v", plan.Audio, plan.VideoSources())
	}
}

func TestCameraOffAndStalledTracksBecomeCards(t *testing.T) {
	r, a, b := stageRoom(t, LayoutAuto)
	a.State.CameraOff = true
	stalled := func(ref SourceRef) bool { return ref != SourceRef{b.ID, SourceCamera} }
	plan := PlanComposition(r, DefaultOutput, stalled)
	if plan.Tiles[0].Kind != TileCard || plan.Tiles[1].Kind != TileCard {
		t.Fatalf("expected two cards, got %+v", plan.Tiles)
	}
	if len(plan.VideoSources()) != 0 || len(plan.Audio) != 2 {
		t.Fatalf("video %v audio %v", plan.VideoSources(), plan.Audio)
	}
}

func TestOffStageParticipantsAreExcluded(t *testing.T) {
	r, a, _ := stageRoom(t, LayoutAuto)
	a.OnStage = false
	plan := PlanComposition(r, DefaultOutput, allLive)
	if len(plan.Tiles) != 1 || plan.Tiles[0].Label != "Beto" || len(plan.Audio) != 1 {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestScreenShareTakesFocus(t *testing.T) {
	r, _, b := stageRoom(t, LayoutAuto)
	b.State.ScreenSharing = true
	r.SetPublished(b.ID, SourceScreen, true)
	r.SetPublished(b.ID, SourceScreenAudio, true)
	plan := PlanComposition(r, DefaultOutput, allLive)
	if len(plan.Tiles) != 3 || !plan.Tiles[0].Focus || plan.Tiles[0].Label != "Pantalla de Beto" {
		t.Fatalf("tiles = %+v", plan.Tiles)
	}
	main, side := plan.Tiles[0].Rect, plan.Tiles[1].Rect
	if main.W <= side.W*2 || side.X <= main.X+main.W {
		t.Fatalf("focus %+v must be large and left of the column %+v", main, side)
	}
	if len(plan.Audio) != 3 {
		t.Fatalf("screen audio must be mixed: %v", plan.Audio)
	}

	r.Settings.Layout = LayoutGrid
	grid := PlanComposition(r, DefaultOutput, allLive)
	if grid.Tiles[0].Focus || grid.Tiles[0].Rect.W != grid.Tiles[1].Rect.W {
		t.Fatalf("grid must not focus: %+v", grid.Tiles)
	}
	if grid.Signature() == plan.Signature() {
		t.Fatal("different layouts must have different signatures")
	}

	// A share that stopped (flag off) is not shown even if the track exists.
	b.State.ScreenSharing = false
	r.Settings.Layout = LayoutAuto
	if p := PlanComposition(r, DefaultOutput, allLive); len(p.Tiles) != 2 || p.Tiles[0].Focus {
		t.Fatalf("stopped share still shown: %+v", p.Tiles)
	}
}

func TestSpeakerLayout(t *testing.T) {
	r, _, _ := stageRoom(t, LayoutSpeaker)
	plan := PlanComposition(r, DefaultOutput, allLive)
	if !plan.Tiles[0].Focus || plan.Tiles[0].Label != "Ana" || len(plan.Tiles) != 2 {
		t.Fatalf("speaker = %+v", plan.Tiles)
	}
	only := newTestRoom(t, 0)
	only.Settings.Layout = LayoutSpeaker
	p, _ := only.Join("Solo", "", true, now)
	only.SetPublished(p.ID, SourceCamera, true)
	solo := PlanComposition(only, DefaultOutput, allLive)
	if len(solo.Tiles) != 1 || solo.Tiles[0].Rect.W < 1200 {
		t.Fatalf("a lone speaker fills the canvas: %+v", solo.Tiles)
	}
}

func TestGridLayoutCentresLastRow(t *testing.T) {
	rects := gridLayout(3, 1280, 720, 12)
	if len(rects) != 3 || rects[2].Y <= rects[0].Y {
		t.Fatalf("rects = %+v", rects)
	}
	centre := rects[2].X + rects[2].W/2
	if abs(centre-640) > 4 {
		t.Fatalf("last row not centred: %+v", rects[2])
	}
	if gridLayout(0, 1280, 720, 12) != nil {
		t.Fatal("no tiles expected")
	}
	if (SourceRef{"p-1", SourceCamera}).String() != "p-1/camera" {
		t.Fatal("source ref string")
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
