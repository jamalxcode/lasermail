package engine

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jamalxcode/lasermail/internal/imapsync"
	"github.com/jamalxcode/lasermail/internal/store"
)

// worker syncs one account: a full pass over its folders, then IDLE on the
// inbox, a full pass again every FullSyncEvery or when asked.
type worker struct {
	e      *Engine
	id     int64
	cancel context.CancelFunc
	done   chan struct{}
	kick   chan struct{}

	actMu   sync.Mutex
	actConn *imapsync.Conn // a second connection for user actions
}

func (e *Engine) startWorker(a *store.Account) {
	if e.cfg.Offline {
		e.mu.Lock()
		e.status[a.ID] = &AccountStatus{State: "idle", LastSync: time.Now()}
		e.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(e.ctx)
	w := &worker{e: e, id: a.ID, cancel: cancel, done: make(chan struct{}), kick: make(chan struct{}, 1)}
	e.mu.Lock()
	e.workers[a.ID] = w
	e.mu.Unlock()
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		defer close(w.done)
		w.run(ctx)
	}()
}

func (w *worker) stop() {
	w.cancel()
	w.closeActionConn()
	<-w.done
}

func (w *worker) closeActionConn() {
	w.actMu.Lock()
	if w.actConn != nil {
		w.actConn.Close()
		w.actConn = nil
	}
	w.actMu.Unlock()
}

// SyncNow asks an account (0 = all) to sync right away.
func (e *Engine) SyncNow(id int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for wid, w := range e.workers {
		if id == 0 || wid == id {
			select {
			case w.kick <- struct{}{}:
			default:
			}
		}
	}
}

func (w *worker) run(ctx context.Context) {
	backoff := 5 * time.Second
	for ctx.Err() == nil {
		err := w.session(ctx)
		if ctx.Err() != nil {
			return
		}
		w.e.setStatus(w.id, "error", err)
		w.e.cfg.Logf("account %d: %v (retrying in %v)", w.id, err, backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		case <-w.kick:
		}
		backoff = min(backoff*2, 5*time.Minute)
	}
}

// session is one connection's life: sync, then idle until something happens.
func (w *worker) session(ctx context.Context) error {
	a, err := w.e.st.Account(w.id)
	if err != nil {
		return err
	}
	w.e.setStatus(w.id, "connecting", nil)
	c, err := w.e.dial(ctx, a)
	if err != nil {
		return err
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()

	var kicked atomic.Bool
	full := true
	lastFull := time.Time{}
	for ctx.Err() == nil {
		w.e.setStatus(w.id, "syncing", nil)
		if kicked.Swap(false) {
			full = true
		}
		if full || time.Since(lastFull) > w.e.cfg.FullSyncEvery {
			if err := w.syncAll(c); err != nil {
				return err
			}
			lastFull = time.Now()
			full = false
		} else if err := w.syncRole(c, store.Inbox); err != nil {
			return err
		}
		w.e.setStatus(w.id, "idle", nil)
		inbox, err := w.e.st.MailboxByRole(w.id, store.Inbox)
		if err != nil {
			return err
		}
		if _, err := c.Select(inbox.Path); err != nil {
			return err
		}
		// IDLE until the server reports a change, a kick, or the next full pass.
		ictx, cancel := context.WithTimeout(ctx, max(time.Until(lastFull.Add(w.e.cfg.FullSyncEvery)), time.Second))
		watcher := make(chan struct{})
		go func() {
			defer close(watcher)
			select {
			case <-w.kick:
				kicked.Store(true)
				cancel()
			case <-ictx.Done():
			}
		}()
		_, err = c.Idle(ictx)
		cancel()
		// Wait for the watcher: a kick it takes after IDLE ended must be seen
		// by the next pass, not lost until the next full sync.
		<-watcher
		if err != nil && ctx.Err() == nil && ictx.Err() == nil {
			return err
		}
	}
	return ctx.Err()
}

// syncedRole: Gmail's All Mail repeats every other folder, so it is not
// synced (search and archive still work through it on the server).
func syncedRole(role string) bool { return role != store.All }

func (w *worker) syncAll(c *imapsync.Conn) error {
	boxes, err := c.SyncFolders(w.e.st, w.id)
	if err != nil {
		return err
	}
	// Inbox first so new mail shows up before the long tail.
	for pass := 0; pass < 2; pass++ {
		for _, mb := range boxes {
			if (pass == 0) != (mb.Role == store.Inbox) || !syncedRole(mb.Role) {
				continue
			}
			if err := w.syncBox(c, mb); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *worker) syncRole(c *imapsync.Conn, role string) error {
	mb, err := w.e.st.MailboxByRole(w.id, role)
	if err != nil {
		return err
	}
	return w.syncBox(c, mb)
}

func (w *worker) syncBox(c *imapsync.Conn, mb *store.Mailbox) error {
	o := w.e.syncOpt
	if mb.Role == store.Other || mb.Role == store.Trash || mb.Role == store.Junk {
		o.Window = min(o.Window, 500)
		o.Prefetch = min(o.Prefetch, 30)
	}
	res, err := c.SyncMailbox(w.e.st, mb, o)
	if err != nil {
		return err
	}
	w.e.emit(Event{Type: "sync", AccountID: w.id, MailboxID: mb.ID, Count: res.New})
	if res.New > 0 && mb.Role == store.Inbox && mb.LastUID > 0 {
		w.e.emit(Event{Type: "new_mail", AccountID: w.id, MailboxID: mb.ID, Count: res.New})
	}
	return nil
}

// withConn runs fn on the account's action connection, reconnecting once
// if it dropped.
func (e *Engine) withConn(ctx context.Context, accountID int64, fn func(*imapsync.Conn) error) error {
	e.mu.Lock()
	w := e.workers[accountID]
	e.mu.Unlock()
	if w == nil {
		return store.ErrNotFound
	}
	w.actMu.Lock()
	defer w.actMu.Unlock()
	for attempt := 0; ; attempt++ {
		if w.actConn == nil {
			a, err := e.st.Account(accountID)
			if err != nil {
				return err
			}
			if w.actConn, err = e.dial(ctx, a); err != nil {
				return err
			}
		}
		err := fn(w.actConn)
		if err == nil {
			return nil
		}
		select {
		case <-w.actConn.Closed():
			w.actConn = nil
			if attempt == 0 {
				continue
			}
		default:
		}
		return err
	}
}
