// Package engine defines the event types used by the discrete-event simulator.
//
// Every action in the simulation is an Event. Events are placed into a
// priority queue (min-heap) ordered by SimTime. When two events have the
// same SimTime, the Seq (sequence number) breaks the tie deterministically.
// This guarantees byte-identical output for repeated runs with the same seed.
package engine

// EventType labels what kind of simulation event has occurred.
type EventType string

const (
	// EventBlockPropose fires when a node creates a new block.
	// This is the starting point: the proposing node adds the block to its
	// own chain and then schedules BlockGossip events to each peer.
	EventBlockPropose EventType = "BlockPropose"

	// EventBlockGossip represents a block "in flight" from one node to another.
	// It models network transmission: the message has been sent but not yet
	// received. When this event fires, we schedule a BlockArrive event with
	// an additional propagation processing delay.
	EventBlockGossip EventType = "BlockGossip"

	// EventBlockArrive fires when a block message is finally received and
	// processed by the destination node. The node validates and (if valid)
	// adopts the block, possibly switching its chain tip.
	EventBlockArrive EventType = "BlockArrive"

	// EventBlockReject fires when a block arrival is rejected due to invalidity.
	EventBlockReject EventType = "BlockReject"

	// EventBlockOrphan fires when a block arrives whose parent is unknown,
	// causing the block to be buffered awaiting its parent.
	EventBlockOrphan EventType = "BlockOrphan"

	// EventTipUpdate fires after a node's tip changes due to a new block.
	// Logged for observability; diff visibility of chain tip switches.
	EventTipUpdate EventType = "TipUpdate"

	// EventChurnToggle fires when a node transitions between online and offline
	// states in the Markov on/off peer availability model (Milestone 2).
	EventChurnToggle EventType = "ChurnToggle"

	// EventConfigChange fires when a dynamic network event is applied mid-run.
	// It records the block number that triggered the change, the field name, and
	// the new value, providing an independently auditable record of parameter shifts.
	EventConfigChange EventType = "ConfigChange"

	// EventTxArrival fires when a transaction arrives into the network mempool.
	// Models transaction arrivals independently of block proposal frequency.
	EventTxArrival EventType = "TxArrival"
)

// Event is a single simulation action scheduled at a specific simulated time.
type Event struct {
	// SimTime is the simulated clock time (in seconds) when this event fires.
	SimTime float64

	// Seq is a monotonically increasing counter assigned at scheduling time.
	// It guarantees a total order even when two events have the same SimTime.
	Seq int64

	// Type identifies what kind of event this is.
	Type EventType

	// NodeID is the node that processes this event (the recipient / actor).
	NodeID string

	// FromID is the sending node (for gossip and arrival events).
	FromID string

	// Block is the block involved in this event (may be zero-value for some types).
	Block interface{} // holds node.Block; using interface to avoid import cycle
}

// EventQueue is a min-heap of Events ordered by (SimTime, Seq).
// It implements heap.Interface from the standard library's container/heap.
//
// The heap invariant: parent.SimTime ≤ child.SimTime
// (or parent.Seq < child.Seq when times are equal).
// This means the smallest (earliest) event is always at index 0.
type EventQueue []*Event

// Len implements heap.Interface.
func (q EventQueue) Len() int { return len(q) }

// Less implements heap.Interface.
// Returns true if event i should be processed before event j.
// Primary sort key: SimTime (ascending). Tie-break: Seq (ascending).
func (q EventQueue) Less(i, j int) bool {
	if q[i].SimTime != q[j].SimTime {
		return q[i].SimTime < q[j].SimTime
	}
	return q[i].Seq < q[j].Seq
}

// Swap implements heap.Interface.
func (q EventQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }

// Push implements heap.Interface. Appends a new event to the slice.
// Do not call this directly — use heap.Push(q, event) instead.
func (q *EventQueue) Push(x interface{}) {
	*q = append(*q, x.(*Event))
}

// Pop implements heap.Interface. Removes and returns the smallest event.
// Do not call this directly — use heap.Pop(q) instead.
func (q *EventQueue) Pop() interface{} {
	old := *q
	n := len(old)
	item := old[n-1]
	old[n-1] = nil // avoid memory leak
	*q = old[:n-1]
	return item
}
