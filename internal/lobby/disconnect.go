package lobby

import "time"

// disconnectGrace holds mid-game seats after a dropped session. Caller must hold
// Manager.mu around every method: the maps are not independently locked.
type disconnectGrace struct {
	pending  map[string]*time.Timer
	expiring map[string]struct{}
}

func newDisconnectGrace() disconnectGrace {
	return disconnectGrace{
		pending:  make(map[string]*time.Timer),
		expiring: make(map[string]struct{}),
	}
}

func (d *disconnectGrace) clear(id string) {
	if t, ok := d.pending[id]; ok {
		t.Stop()
		delete(d.pending, id)
	}
	delete(d.expiring, id)
}

func (d *disconnectGrace) arm(id string, wait time.Duration, fire func()) {
	if t, ok := d.pending[id]; ok {
		t.Stop()
	}
	d.pending[id] = time.AfterFunc(wait, fire)
}

// beginExpire moves a pending leave into the expiring set. False if there was
// nothing to expire (already resumed or cleared).
func (d *disconnectGrace) beginExpire(id string) bool {
	if _, ok := d.pending[id]; !ok {
		return false
	}
	delete(d.pending, id)
	d.expiring[id] = struct{}{}
	return true
}

// tryCancel stops a pending leave for reconnect. blocked means expire already
// owns the seat (or the timer callback is in flight).
func (d *disconnectGrace) tryCancel(id string) (cancelled, blocked bool) {
	if _, ok := d.expiring[id]; ok {
		return false, true
	}
	t, ok := d.pending[id]
	if !ok {
		return false, false
	}
	if !t.Stop() {
		return false, true
	}
	delete(d.pending, id)
	return true, false
}
