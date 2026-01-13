// SPDX-License-Identifier: Unlicense OR MIT

/*
Package pointer implements pointer events and operations.
A pointer is either a mouse controlled cursor or a touch
object such as a finger.

The [event.Op] operation is used to declare a handler ready for pointer
events.

# Hit areas

Clip operations from package [op/clip] are used for specifying
hit areas where handlers may receive events.

For example, to set up a handler with a rectangular hit area:

	r := image.Rectangle{...}
	area := clip.Rect(r).Push(ops)
	event.Op{Tag: h}.Add(ops)
	area.Pop()

Note that hit areas behave similar to painting: the effective area of a stack
of multiple area operations is the intersection of the areas.

BUG: Clip operations other than clip.Rect and clip.Ellipse are approximated
with their bounding boxes.

# Matching events

Areas form an implicit tree, with input handlers as leaves. The children of
an area consist of every area and handler added between its Push and corresponding Pop.

For example:

	ops := new(op.Ops)
	h1, h2, h3, h4 := "h1", "h2", "h3", "h4"

	area := clip.Rect(image.Rect(0, 0, 100, 100))
	root := area.Push(ops)

	event.Op(ops, &h1)
	event.Op(ops, h1)

	child1 := area.Push(ops)
	event.Op(ops, h2)
	child1.Pop()

	child2 := area.Push(ops)
	event.Op(ops, h3)
	event.Op(ops, h4)
	child2.Pop()

	root.Pop()

This implies a tree with five handler nodes as illustrated below:

		root
		|
		&h1
		|
		h1
		|------------+
		|            |
	    child1       child2
		|            |
		h2           h3
		             |
		             h4

Event matching proceeds as follows:

Every handler attached to an area is matched with the event during a
depth-first traversal of the tree, following a rightmost-first expansion policy.

In the example above, the processing order is: h4, h3, h2, h1, &h1

# Event Passing

Events pass through sibling and ancestor areas by default.

To stop event propagation, use event.StopOp to declare a terminating handler.
When a terminating handler matches an event, it stops propagation to subsequent
handlers in the processing order.

To intercept events from third-party widgets that use event.Op (where you cannot
modify them to use event.StopOp), wrap them with StopOp.Push and
the corresponding StopStack.Pop. This allows interception without needing to calculate
the widget's precise clip area.

# Disambiguation

When more than one handler matches a pointer event, the event queue
follows a set of rules for distributing the event.

As long as the pointer has not received a Press event, all
matching handlers receive all events.

When a pointer is pressed, the set of matching handlers is
recorded. The set is not updated according to the pointer position
and hit areas. Rather, handlers stay in the matching set until they
no longer appear in a InputOp or when another handler in the set
grabs the pointer.

A handler can exclude all other handler from its matching sets
by setting the Grab flag in its InputOp. The Grab flag is sticky
and stays in effect until the handler no longer appears in any
matching sets.

The losing handlers are notified by a Cancel event.

For multiple grabbing handlers, the foremost handler wins.

# Priorities

Handlers know their position in a matching set of a pointer through
event priorities. The Shared priority is for matching sets with
multiple handlers; the Grabbed priority indicate exclusive access.

Priorities are useful for deferred gesture matching.

Consider a scrollable list of clickable elements. When the user touches an
element, it is unknown whether the gesture is a click on the element
or a drag (scroll) of the list. While the click handler might light up
the element in anticipation of a click, the scrolling handler does not
scroll on finger movements with lower than Grabbed priority.

Should the user release the finger, the click handler registers a click.

However, if the finger moves beyond a threshold, the scrolling handler
determines that the gesture is a drag and sets its Grab flag. The
click handler receives a Cancel (removing the highlight) and further
movements for the scroll handler has priority Grabbed, scrolling the
list.
*/
package pointer
