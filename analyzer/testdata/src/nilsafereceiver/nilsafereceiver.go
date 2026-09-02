// Package nilsafereceiver isolates the nilSafeReceiver fact: which
// pointer-receiver methods a nil receiver can safely call, and which cannot.
package nilsafereceiver

type box struct{ n int }

// guarded checks its own receiver before touching it, so it tolerates a nil
// receiver directly.
func (b *box) guarded() bool { // want guarded:"nil-safe receiver"
	if b == nil {
		return true
	}

	return b.n > 0
}

// dereferences its receiver with no guard, so it does not tolerate nil.
func (b *box) dereferences() int {
	return b.n
}

// delegating only calls another method that itself tolerates a nil receiver,
// which makes it nil-safe by delegation even though it never guards directly.
func (b *box) delegating() bool { // want delegating:"nil-safe receiver"
	return b.guarded()
}

// callsUnsafe calls a method that dereferences the receiver, so the call
// itself counts as a dereference and callsUnsafe does not qualify.
func (b *box) callsUnsafe() int {
	return b.dereferences()
}
