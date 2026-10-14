package domain

import (
	"fmt"
	"math"
	"strings"
)

// SourceRef names one published track in a room.
type SourceRef struct {
	Participant ParticipantID `json:"participant"`
	Source      MediaSource   `json:"source"`
}

func (s SourceRef) String() string { return fmt.Sprintf("%s/%s", s.Participant, s.Source) }

// Rect is a pixel rectangle on the output canvas; all values are even so
// that yuv420p scaling never needs rounding.
type Rect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// TileKind is what a tile renders.
type TileKind string

const (
	// TileVideo renders a live track.
	TileVideo TileKind = "video"
	// TileCard renders a name card (camera off, or the track stalled).
	TileCard TileKind = "card"
)

// Tile is one element of the composed picture.
type Tile struct {
	Rect   Rect      `json:"rect"`
	Kind   TileKind  `json:"kind"`
	Source SourceRef `json:"source"`
	Label  string    `json:"label"`
	Focus  bool      `json:"focus"`
}

// CompositionPlan is the complete description of the broadcast picture and
// mix at one point in time. The compositor adapter turns it into an encoder
// pipeline; equal signatures mean nothing has to change.
type CompositionPlan struct {
	Output OutputSettings `json:"output"`
	// Title is drawn when nobody is on stage.
	Title string      `json:"title"`
	Tiles []Tile      `json:"tiles"`
	Audio []SourceRef `json:"audio"`
}

// VideoSources lists the tracks the picture needs, in tile order.
func (p CompositionPlan) VideoSources() []SourceRef {
	var refs []SourceRef
	for _, t := range p.Tiles {
		if t.Kind == TileVideo {
			refs = append(refs, t.Source)
		}
	}
	return refs
}

// Signature is a stable textual form used to detect changes.
func (p CompositionPlan) Signature() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%dx%d@%d/%d/%d|%s|", p.Output.Width, p.Output.Height, p.Output.Framerate,
		p.Output.VideoBitrate, p.Output.AudioBitrate, p.Title)
	for _, t := range p.Tiles {
		fmt.Fprintf(&b, "%s:%s:%d,%d,%d,%d:%s:%t;", t.Kind, t.Source, t.Rect.X, t.Rect.Y, t.Rect.W, t.Rect.H, t.Label, t.Focus)
	}
	b.WriteString("|")
	for _, a := range p.Audio {
		b.WriteString(a.String() + ";")
	}
	return b.String()
}

// LiveFunc reports whether a published track is currently delivering media.
// A track that stalls is shown as a card so the picture never freezes.
type LiveFunc func(SourceRef) bool

const layoutGap = 12

type stageItem struct {
	kind   TileKind
	source SourceRef
	label  string
	screen bool
}

// PlanComposition lays out the participants on stage according to the
// room's layout mode.
func PlanComposition(room *Room, output OutputSettings, live LiveFunc) CompositionPlan {
	plan := CompositionPlan{Output: output, Title: room.Settings.Title, Tiles: []Tile{}, Audio: []SourceRef{}}
	isLive := func(p *Participant, s MediaSource) bool {
		return p.Published[s] && live(SourceRef{p.ID, s})
	}

	var people, screens []stageItem
	var speaker []stageItem
	for i, p := range room.Participants() {
		if !p.OnStage {
			continue
		}
		person := stageItem{kind: TileCard, source: SourceRef{p.ID, SourceCamera}, label: p.Name}
		if !p.State.CameraOff && isLive(p, SourceCamera) {
			person.kind = TileVideo
		}
		people = append(people, person)
		sharing := p.State.ScreenSharing && isLive(p, SourceScreen)
		if sharing {
			screens = append(screens, stageItem{
				kind: TileVideo, source: SourceRef{p.ID, SourceScreen},
				label: "Pantalla de " + p.Name, screen: true,
			})
		}
		if isLive(p, SourceMicrophone) {
			plan.Audio = append(plan.Audio, SourceRef{p.ID, SourceMicrophone})
		}
		if sharing && isLive(p, SourceScreenAudio) {
			plan.Audio = append(plan.Audio, SourceRef{p.ID, SourceScreenAudio})
		}
		if i == firstOnStage(room) {
			if sharing {
				speaker = append(speaker, screens[len(screens)-1])
			} else {
				speaker = append(speaker, person)
			}
		}
	}

	var focus *stageItem
	var rest []stageItem
	switch room.Settings.Layout {
	case LayoutGrid:
		rest = append(append(rest, screens...), people...)
	case LayoutSpeaker:
		if len(speaker) > 0 {
			focus = &speaker[0]
			for _, it := range append(append([]stageItem{}, screens...), people...) {
				if it.source != focus.source {
					rest = append(rest, it)
				}
			}
		}
	default: // auto
		if len(screens) > 0 {
			focus = &screens[0]
			rest = append(append(rest, screens[1:]...), people...)
		} else {
			rest = people
		}
	}

	w, h := output.Width, output.Height
	if focus == nil {
		for i, r := range gridLayout(len(rest), w, h, layoutGap) {
			plan.Tiles = append(plan.Tiles, rest[i].tile(r, false))
		}
		return plan
	}
	main, side := spotlightLayout(len(rest), w, h, layoutGap)
	plan.Tiles = append(plan.Tiles, focus.tile(main, true))
	for i, r := range side {
		plan.Tiles = append(plan.Tiles, rest[i].tile(r, false))
	}
	return plan
}

func firstOnStage(room *Room) int {
	for i, p := range room.Participants() {
		if p.OnStage {
			return i
		}
	}
	return -1
}

func (it stageItem) tile(r Rect, focus bool) Tile {
	return Tile{Rect: r, Kind: it.kind, Source: it.source, Label: it.label, Focus: focus}
}

// fitContain is the largest 16:9 rect centred inside (w, h).
func fitContain(x, y, w, h int) Rect {
	scale := math.Min(float64(w)/16, float64(h)/9)
	tw, th := even(16*scale), even(9*scale)
	return Rect{X: evenInt(x + (w-tw)/2), Y: evenInt(y + (h-th)/2), W: tw, H: th}
}

// gridLayout mirrors the webrelay's mosaic: ceil(sqrt(n)) columns, 16:9
// cells, centred, last row centred.
func gridLayout(n, w, h, gap int) []Rect {
	if n <= 0 {
		return nil
	}
	cols := int(math.Ceil(math.Sqrt(float64(n))))
	rows := int(math.Ceil(float64(n) / float64(cols)))
	cellW := (w - gap*(cols+1)) / cols
	cellH := (h - gap*(rows+1)) / rows
	tile := fitContain(0, 0, cellW, cellH)
	usedW := cols*tile.W + (cols-1)*gap
	usedH := rows*tile.H + (rows-1)*gap
	left, top := (w-usedW)/2, (h-usedH)/2
	rects := make([]Rect, n)
	for i := range n {
		row := i / cols
		inRow := cols
		if row == rows-1 {
			inRow = n - row*cols
		}
		offset := (cols - inRow) * (tile.W + gap) / 2
		rects[i] = Rect{
			X: evenInt(left + offset + (i%cols)*(tile.W+gap)),
			Y: evenInt(top + row*(tile.H+gap)),
			W: tile.W, H: tile.H,
		}
	}
	return rects
}

// spotlightLayout puts the focus on the left and stacks the rest in a right
// column (a quarter of the width).
func spotlightLayout(n, w, h, gap int) (Rect, []Rect) {
	if n == 0 {
		return fitContain(gap, gap, w-2*gap, h-2*gap), nil
	}
	sideW := w / 4
	main := fitContain(gap, gap, w-sideW-3*gap, h-2*gap)
	thumbH := min(sideW*9/16, (h-gap*(n+1))/n)
	thumbW := thumbH * 16 / 9
	thumbH, thumbW = evenInt(thumbH), evenInt(thumbW)
	usedH := n*thumbH + (n-1)*gap
	top := (h - usedH) / 2
	x := w - gap - sideW + (sideW-thumbW)/2
	side := make([]Rect, n)
	for i := range n {
		side[i] = Rect{X: evenInt(x), Y: evenInt(top + i*(thumbH+gap)), W: thumbW, H: thumbH}
	}
	return main, side
}

func even(v float64) int { return evenInt(int(v)) }
func evenInt(v int) int  { return v &^ 1 }
