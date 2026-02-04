// SPDX-License-Identifier: Unlicense OR MIT

package layout

import (
	"image"
	"math"

	"gioui.org/gesture"
	"gioui.org/io/pointer"
	"gioui.org/op"
	"gioui.org/op/clip"
)

type scrollChild struct {
	size image.Point
	call op.CallOp
}

// List displays a subsection of a potentially infinitely
// large underlying list. List accepts user input to scroll
// the subsection.
type List struct {
	Axis Axis
	// ScrollToEnd instructs the list to stay scrolled to the far end position
	// once reached. A List with ScrollToEnd == true draws its content with
	// the last item at the bottom of the list area.
	ScrollToEnd bool
	// Alignment is the cross axis alignment of list elements.
	Alignment Alignment
	// ScrollAnyAxis allows any scroll axis to scroll the list, not just the main axis.
	ScrollAnyAxis bool
	// Gap is the space in pixels between children.
	Gap int

	cs          Constraints
	scroll      gesture.Scroll
	scrollDelta int

	// Position is updated during Layout. To save the list scroll position,
	// just save Position after Layout finishes. To scroll the list
	// programmatically, update Position (e.g. restore it from a saved value)
	// before calling Layout.
	Position Position

	len int

	// maxSize is the total size of visible children.
	maxSize  int
	children []scrollChild
	dir      iterationDir
}

// ListElement is a function that computes the dimensions of
// a list element.
type ListElement func(gtx Context, index int) Dimensions

type iterationDir uint8

// Position is a List scroll offset represented as an offset from the top edge
// of a child element.
type Position struct {
	// BeforeEnd tracks whether the List position is before the very end. We
	// use "before end" instead of "at end" so that the zero value of a
	// Position struct is useful.
	//
	// When laying out a list, if ScrollToEnd is true, then First and Offset
	// are ignored, and the list is drawn with the last item at the bottom.
	BeforeEnd bool
	// First is the index of the first visible child.
	First int
	// Offset is the distance in pixels from the leading edge to the child at index
	// First.
	Offset int
	// OffsetLast is the signed distance in pixels from the trailing edge to the
	// bottom edge of the child at index First+Count.
	OffsetLast int
	// Count is the number of visible children.
	Count int
	// Length is the estimated total size of all children, measured in pixels.
	Length int
}

const (
	iterateNone iterationDir = iota
	iterateForward
	iterateBackward
)

const inf = 1e6

// init prepares the list for iterating through its children with next.
func (l *List) init(gtx Context, len int) {
	if l.more() {
		panic("unfinished child")
	}
	l.cs = gtx.Constraints
	l.maxSize = 0
	l.children = l.children[:0]
	l.len = len
	l.update(gtx)
	l.checkAndResetPosition()
}

func (l *List) checkAndResetPosition() {
	if l.Position.First < 0 {
		l.Position.Offset = 0
		l.Position.First = 0
	}
	if l.scrollToEnd() || l.Position.First > l.len {
		l.Position.Offset = 0
		l.Position.First = l.len
	}
}

// Layout a List of len items, where each item is implicitly defined
// by the callback w. Layout can handle very large lists because it only calls
// w to fill its viewport and the distance scrolled, if any.
func (l *List) Layout(gtx Context, len int, w ListElement) Dimensions {
	l.init(gtx, len)
	crossMin, crossMax := l.Axis.crossConstraint(gtx.Constraints)
	gtx.Constraints = l.Axis.constraints(0, inf, crossMin, crossMax)
	macro := op.Record(gtx.Ops)
	laidOutTotalLength := 0
	numLaidOut := 0

	for l.next(); l.more(); l.next() {
		child := op.Record(gtx.Ops)
		dims := w(gtx, l.index())
		call := child.Stop()
		l.end(dims, call)
		laidOutTotalLength += l.Axis.Convert(dims.Size).X
		numLaidOut++
	}

	if numLaidOut > 0 {
		l.Position.Length = laidOutTotalLength * len / numLaidOut + l.Gap*(len-1)
	} else {
		l.Position.Length = 0
	}
	return l.layout(gtx.Ops, macro)
}

func (l *List) scrollToEnd() bool {
	return l.ScrollToEnd
}

// Dragging reports whether the List is being dragged.
func (l *List) Dragging() bool {
	return l.scroll.State() == gesture.StateDragging
}

func (l *List) update(gtx Context) {
	min, max := int(-inf), int(inf)
	if l.Position.First == 0 {
		// Use the size of the invisible part as scroll boundary.
		min = -l.Position.Offset
		if min > 0 {
			min = 0
		}
	}
	if l.Position.First+l.Position.Count == l.len {
		max = -l.Position.OffsetLast
		if max < 0 {
			max = 0
		}
	}

	xrange := pointer.ScrollRange{Min: min, Max: max}
	yrange := pointer.ScrollRange{}

	axis := gesture.Axis(l.Axis)
	if l.ScrollAnyAxis {
		axis = gesture.Both
		yrange = xrange
	} else if l.Axis == Vertical {
		xrange, yrange = yrange, xrange
	}
	d := l.scroll.Update(gtx.Metric, gtx.Source, gtx.Now, axis, xrange, yrange)

	l.scrollDelta = d
	l.Position.Offset += d
}

// next advances to the next child.
func (l *List) next() {
	l.dir = l.nextDir()
	// The user scroll offset is applied after scrolling to
	// list end.
	if l.scrollToEnd() && !l.more() && l.scrollDelta < 0 {
		l.Position.BeforeEnd = true
		l.Position.Offset += l.scrollDelta
		l.dir = l.nextDir()
	}
}

// index is current child's position in the underlying list.
func (l *List) index() int {
	switch l.dir {
	case iterateBackward:
		return l.Position.First - 1
	case iterateForward:
		return l.Position.First + len(l.children)
	default:
		panic("Index called before Next")
	}
}

// more reports whether more children are needed.
func (l *List) more() bool {
	return l.dir != iterateNone
}

func (l *List) nextDir() iterationDir {
	//-----------------------------position.offset
	// l.maxSize = s1 + s2        |		  |
	//--------top edge, offset: 0 |       |
	// backward space             | forward space
	//--------position.offset     |       |
	// forward space              |       |
	//--------leading edge of children, offset: - viewSize + l.maxSize
	// child1  size: s1
	//--------offset: - viewSize + s2
	// child2  size: s2
	//--------bottom edge, offset: - viewSize
	l.tryAlignToBottomEdge()
	l.tryAlignToTopEdge()
	switch {
	case len(l.children) == l.len:
		return iterateNone
	case l.forwardSpaceAvailable():
		return iterateForward
	case l.backwardSpaceAvailable():
		return iterateBackward
	}
	return iterateNone
}

func (l *List) tryAlignToTopEdge() {
	if l.firstElemVisible() && l.backwardSpaceAvailable() {
		l.clearBackwardSpace()
	}
}

func (l *List) tryAlignToBottomEdge() {
	if l.lastElemVisible() && l.forwardSpaceAvailable() {
		l.clearForwardSpace()
	}
}

func (l *List) firstElemVisible() bool {
	return l.Position.First == 0
}

func (l *List) lastElemVisible() bool {
	last := l.Position.First + len(l.children)
	lastVisible := last == l.len
	return lastVisible
}

func (l *List) backwardSpaceAvailable() bool {
	return -l.Position.Offset > 0
}

func (l *List) forwardSpaceAvailable() bool {
	return l.Position.Offset-l.leadingEdgeOfChildren() > 0
}

func (l *List) clearBackwardSpace() {
	l.Position.Offset = 0
}

func (l *List) clearForwardSpace() {
	l.Position.Offset = l.leadingEdgeOfChildren()
}

func (l *List) leadingEdgeOfChildren() int {
	_, viewSize := l.Axis.mainConstraint(l.cs)
	return l.maxSize - viewSize
}

// End the current child by specifying its dimensions.
func (l *List) end(dims Dimensions, call op.CallOp) {
	child := scrollChild{dims.Size, call}
	mainSize := l.Axis.Convert(child.size).X
	if len(l.children) > 0 {
		l.maxSize += l.Gap
	}
	l.maxSize += mainSize
	switch l.dir {
	case iterateForward:
		l.children = append(l.children, child)
	case iterateBackward:
		l.children = append([]scrollChild{child}, l.children...)
		l.Position.First--
		l.Position.Offset += mainSize + l.Gap
	default:
		panic("call Next before End")
	}
	l.dir = iterateNone
}

// Layout the List and return its dimensions.
func (l *List) layout(ops *op.Ops, macro op.MacroOp) Dimensions {
	if l.more() {
		panic("unfinished child")
	}
	mainMin, mainMax := l.Axis.mainConstraint(l.cs)
	children := l.skipInvisibleChildren()
	mainAxisOffset := -l.Position.Offset
	var maxCross int
	for i, child := range children {
		sz := l.Axis.Convert(child.size)
		if c := sz.Y; c > maxCross {
			maxCross = c
		}
		if i > 0{
			mainAxisOffset += l.Gap
		}
		mainAxisOffset += sz.X
	}
	l.Position.Count = len(children)
	l.Position.OffsetLast = mainMax - mainAxisOffset
	// ScrollToEnd lists are end aligned.
	if space := l.Position.OffsetLast; l.ScrollToEnd && space > 0 {
		l.Position.Offset -= space
	}

	layout := func(child scrollChild, main int) int {
		sz := l.Axis.Convert(child.size)
		var cross int
		switch l.Alignment {
		case End:
			cross = maxCross - sz.Y
		case Middle:
			cross = (maxCross - sz.Y) / 2
		}
		pt := l.Axis.Convert(image.Pt(main, cross))
		transition := op.Offset(pt).Push(ops)
		child.call.Add(ops)
		transition.Pop()
		return sz.X
	}

	mainAxisOffset = -l.Position.Offset
	for i, child := range children {
		if i > 0{
			mainAxisOffset += l.Gap
		}
		childSize := layout(child, mainAxisOffset)
		mainAxisOffset += childSize
	}

	atStart := l.Position.First == 0 && l.Position.Offset <= 0
	atEnd := l.Position.First+len(children) == l.len && mainMax >= mainAxisOffset
	if atStart && l.scrollDelta < 0 || atEnd && l.scrollDelta > 0 {
		l.scroll.Stop()
	}
	l.Position.BeforeEnd = !atEnd

	if mainAxisOffset < mainMin {
		mainAxisOffset = mainMin
	}
	if mainAxisOffset > mainMax {
		mainAxisOffset = mainMax
	}
	if crossMin, crossMax := l.Axis.crossConstraint(l.cs); maxCross < crossMin {
		maxCross = crossMin
	} else if maxCross > crossMax {
		maxCross = crossMax
	}
	dims := l.Axis.Convert(image.Pt(mainAxisOffset, maxCross))
	call := macro.Stop()

	defer clip.Rect(image.Rectangle{Max: dims}).Push(ops).Pop()
	l.scroll.Add(ops)

	call.Add(ops)
	return Dimensions{Size: dims}
}

func (l *List) skipInvisibleChildren() []scrollChild {
	children := l.children
	for len(children) > 0 {
		child := children[0]
		sz := child.size
		mainSize := l.Axis.Convert(sz).X
		if l.Position.Offset < mainSize {
			// First child is partially visible.
			break
		}
		l.Position.First++
		l.Position.Offset -= mainSize + l.Gap
		children = children[1:]
	}
	return children
}

// ScrollBy scrolls the list by a relative amount of items.
//
// Fractional scrolling may be inaccurate for items of differing
// dimensions. This includes scrolling by integer amounts if the current
// l.Position.Offset is non-zero.
func (l *List) ScrollBy(num float32) {
	// Split number of items into integer and fractional parts
	i, f := math.Modf(float64(num))

	// Scroll by integer amount of items
	l.Position.First += int(i)

	// Adjust Offset to account for fractional items. If Offset gets so large that it amounts to an entire item, then
	// the layout code will handle that for us and adjust First and Offset accordingly.
	itemHeight := float64(l.Position.Length) / float64(l.len)
	l.Position.Offset += int(math.Round(itemHeight * f))

	// First and Offset can go out of bounds, but the layout code knows how to handle that.

	// Ensure that the list pays attention to the Offset field when the scrollbar drag
	// is started while the bar is at the end of the list. Without this, the scrollbar
	// cannot be dragged away from the end.
	l.Position.BeforeEnd = true
}

// ScrollTo scrolls to the specified item.
func (l *List) ScrollTo(n int) {
	l.Position.First = n
	l.Position.Offset = 0
	l.Position.BeforeEnd = true
}
