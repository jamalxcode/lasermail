// Package daemon maps protocol operations onto the engine.
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jamalxcode/lasermail/internal/engine"
	"github.com/jamalxcode/lasermail/internal/ipc"
	"github.com/jamalxcode/lasermail/internal/msg"
	"github.com/jamalxcode/lasermail/internal/provider"
	"github.com/jamalxcode/lasermail/internal/store"
)

// Version of laserd, set at build time.
var Version = "dev"

type idArgs struct {
	ID int64 `json:"id"`
}

func decode[T any](raw json.RawMessage) (T, error) {
	var v T
	if len(raw) == 0 {
		return v, nil
	}
	err := json.Unmarshal(raw, &v)
	return v, err
}

// Server builds the socket server for an engine.
func Server(e *engine.Engine, oauthAvailable bool) *ipc.Server {
	return &ipc.Server{
		Handle: func(ctx context.Context, op string, args json.RawMessage) (any, error) {
			return handle(ctx, e, oauthAvailable, op, args)
		},
		Subscribe: func(ctx context.Context, send func(any)) {
			ch, stop := e.Subscribe()
			defer stop()
			for {
				select {
				case <-ctx.Done():
					return
				case ev, ok := <-ch:
					if !ok {
						return
					}
					send(ev)
				}
			}
		},
	}
}

func handle(ctx context.Context, e *engine.Engine, oauthAvailable bool, op string, args json.RawMessage) (any, error) {
	switch op {
	case "hello":
		return map[string]any{"protocol": ipc.Version, "version": Version, "oauth": oauthAvailable,
			"presets": map[string]any{"gmail": must(provider.Preset(provider.Gmail)), "icloud": must(provider.Preset(provider.ICloud))}}, nil
	case "accounts":
		accts, err := e.Accounts()
		if err != nil {
			return nil, err
		}
		if accts == nil {
			accts = []*store.Account{}
		}
		return map[string]any{"accounts": accts, "status": e.Status()}, nil
	case "add_account":
		a, err := decode[engine.NewAccount](args)
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(ctx, 6*time.Minute) // OAuth waits for the browser
		defer cancel()
		return e.AddAccount(ctx, a)
	case "remove_account":
		a, err := decode[idArgs](args)
		if err != nil {
			return nil, err
		}
		return true, e.RemoveAccount(a.ID)
	case "update_password":
		a, err := decode[struct {
			ID       int64  `json:"id"`
			Password string `json:"password"`
		}](args)
		if err != nil {
			return nil, err
		}
		return true, e.UpdatePassword(ctx, a.ID, a.Password)
	case "mailboxes":
		a, err := decode[struct {
			AccountID int64 `json:"account_id"`
		}](args)
		if err != nil {
			return nil, err
		}
		boxes, err := e.Mailboxes(a.AccountID)
		if boxes == nil {
			boxes = []*store.Mailbox{}
		}
		return boxes, err
	case "counts":
		return e.Counts()
	case "list":
		q, err := decode[store.Query](args)
		if err != nil {
			return nil, err
		}
		l, err := e.List(q)
		if l == nil {
			l = []*store.Message{}
		}
		return l, err
	case "open":
		a, err := decode[struct {
			ID int64 `json:"id"`
			engine.OpenOptions
		}](args)
		if err != nil {
			return nil, err
		}
		return e.Open(ctx, a.ID, a.OpenOptions)
	case "set_flag":
		a, err := decode[struct {
			ID   int64  `json:"id"`
			Flag string `json:"flag"`
			On   bool   `json:"on"`
		}](args)
		if err != nil {
			return nil, err
		}
		return true, e.SetFlag(ctx, a.ID, a.Flag, a.On)
	case "archive", "delete", "junk":
		a, err := decode[idArgs](args)
		if err != nil {
			return nil, err
		}
		switch op {
		case "archive":
			return true, e.Archive(ctx, a.ID)
		case "delete":
			return true, e.Delete(ctx, a.ID)
		}
		return true, e.MoveToRole(ctx, a.ID, store.Junk)
	case "move":
		a, err := decode[struct {
			ID        int64 `json:"id"`
			MailboxID int64 `json:"mailbox_id"`
		}](args)
		if err != nil {
			return nil, err
		}
		return true, e.MoveTo(ctx, a.ID, a.MailboxID)
	case "reply", "reply_all", "forward":
		a, err := decode[idArgs](args)
		if err != nil {
			return nil, err
		}
		var d *msg.Draft
		var acct int64
		if op == "forward" {
			d, acct, err = e.Forward(ctx, a.ID)
		} else {
			d, acct, err = e.Reply(ctx, a.ID, op == "reply_all")
		}
		if err != nil {
			return nil, err
		}
		return map[string]any{"account_id": acct, "draft": d}, nil
	case "send":
		a, err := decode[struct {
			AccountID int64     `json:"account_id"`
			Draft     msg.Draft `json:"draft"`
			// Paths of files to attach (the app passes what the user picked).
			AttachPaths []string `json:"attach_paths"`
		}](args)
		if err != nil {
			return nil, err
		}
		for _, p := range a.AttachPaths {
			b, err := os.ReadFile(p)
			if err != nil {
				return nil, err
			}
			a.Draft.Files = append(a.Draft.Files, msg.File{Name: filepath.Base(p), Type: contentType(p, b), Data: b})
		}
		ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
		return true, e.Send(ctx, a.AccountID, &a.Draft)
	case "sync":
		a, err := decode[struct {
			AccountID int64 `json:"account_id"`
		}](args)
		if err != nil {
			return nil, err
		}
		e.SyncNow(a.AccountID)
		return true, nil
	case "save_attachment":
		a, err := decode[struct {
			ID    int64  `json:"id"`
			Index int    `json:"index"`
			Dir   string `json:"dir"`
		}](args)
		if err != nil {
			return nil, err
		}
		if a.Dir == "" {
			home, _ := os.UserHomeDir()
			a.Dir = filepath.Join(home, "Downloads")
		}
		p, err := e.SaveAttachment(ctx, a.ID, a.Index, a.Dir)
		return map[string]string{"path": p}, err
	case "inline_part":
		a, err := decode[struct {
			ID        int64  `json:"id"`
			ContentID string `json:"content_id"`
		}](args)
		if err != nil {
			return nil, err
		}
		at, data, err := e.InlinePart(ctx, a.ID, a.ContentID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": at.Type, "data": data}, nil
	case "set_load_images":
		a, err := decode[struct {
			Email string `json:"email"`
			On    bool   `json:"on"`
		}](args)
		if err != nil {
			return nil, err
		}
		return true, e.SetLoadImages(a.Email, a.On)
	}
	return nil, fmt.Errorf("unsupported op %q", op)
}

func must[T any](v T, _ bool) T { return v }
