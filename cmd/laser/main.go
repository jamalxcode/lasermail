// laser is a command-line client for laserd (handy for scripts and for
// trying the engine without the app).
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"golang.org/x/term"

	"github.com/jamalxcode/lasermail/internal/engine"
	"github.com/jamalxcode/lasermail/internal/ipc"
	"github.com/jamalxcode/lasermail/internal/msg"
	"github.com/jamalxcode/lasermail/internal/paths"
	"github.com/jamalxcode/lasermail/internal/store"
)

const usage = `laser — Laser Email from the terminal (talks to laserd)

  laser accounts                         list accounts and their status
  laser add <email> [--oauth] [--name N] add a Gmail/iCloud account (asks for the app password)
  laser remove <account-id>              remove an account and its local mail
  laser ls [--account N] [--role inbox|sent|drafts|archive|trash|junk] [--mailbox ID] [--unread] [--flagged] [-n 30]
  laser search <words…>                  search every account
  laser read <id> [--images]             show a message (marks it read)
  laser flag|unflag|unread <id>
  laser archive|delete|junk <id>
  laser save <id> <attachment#> [dir]    save an attachment (default ~/Downloads)
  laser send --from <account-id> --to a@b[,c@d] [--cc …] [--bcc …] --subject S   (body on stdin)
  laser reply <id> [--all]               reply (body on stdin, quote kept)
  laser mailboxes [account-id]
  laser sync [account-id]
  laser watch                            print events as they happen
`

func main() {
	if len(os.Args) < 2 || os.Args[1] == "-h" || os.Args[1] == "help" || os.Args[1] == "--help" {
		fmt.Print(usage)
		return
	}
	c, err := ipc.Dial(paths.Socket())
	if err != nil {
		fail(err)
	}
	defer c.Close()
	ctx := context.Background()
	cmd, args := os.Args[1], os.Args[2:]
	call := func(op string, in, out any) {
		if err := c.Call(ctx, op, in, out); err != nil {
			fail(err)
		}
	}
	switch cmd {
	case "accounts":
		var r struct {
			Accounts []store.Account                 `json:"accounts"`
			Status   map[string]engine.AccountStatus `json:"status"`
		}
		call("accounts", nil, &r)
		tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tEMAIL\tKIND\tSTATE")
		for _, a := range r.Accounts {
			st := r.Status[fmt.Sprint(a.ID)]
			state := st.State
			if st.Error != "" {
				state += ": " + st.Error
			}
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", a.ID, a.Email, a.Label, state)
		}
		tw.Flush()
	case "add":
		fs := flag.NewFlagSet("add", flag.ExitOnError)
		useOAuth := fs.Bool("oauth", false, "Gmail: sign in with Google in the browser")
		name := fs.String("name", "", "your name, shown to recipients")
		email, rest := firstArg(args)
		fs.Parse(rest)
		if email == "" {
			fail(fmt.Errorf("usage: laser add <email>"))
		}
		n := engine.NewAccount{Email: email, Name: *name, OAuth: *useOAuth}
		if !*useOAuth {
			fmt.Fprint(os.Stderr, "App password (Gmail: myaccount.google.com/apppasswords; iCloud: account.apple.com → App-Specific Passwords): ")
			pw, err := term.ReadPassword(int(syscall.Stdin))
			fmt.Fprintln(os.Stderr)
			if err != nil {
				line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
				pw = []byte(strings.TrimSpace(line))
			}
			n.Password = string(pw)
		}
		var a store.Account
		call("add_account", n, &a)
		fmt.Printf("Added %s (account %d). Syncing…\n", a.Email, a.ID)
	case "remove":
		call("remove_account", map[string]int64{"id": atoi(args, 0)}, nil)
	case "ls", "search":
		fs := flag.NewFlagSet(cmd, flag.ExitOnError)
		q := store.Query{}
		fs.Int64Var(&q.AccountID, "account", 0, "account id")
		fs.Int64Var(&q.MailboxID, "mailbox", 0, "mailbox id")
		fs.StringVar(&q.Role, "role", "", "inbox, sent, drafts, archive, trash, junk")
		fs.BoolVar(&q.Unread, "unread", false, "unread only")
		fs.BoolVar(&q.Flagged, "flagged", false, "flagged only")
		fs.IntVar(&q.Limit, "n", 30, "how many")
		if cmd == "search" {
			var words []string
			for len(args) > 0 && !strings.HasPrefix(args[0], "-") {
				words = append(words, args[0])
				args = args[1:]
			}
			q.Text = strings.Join(words, " ")
		}
		fs.Parse(args)
		if cmd == "ls" && q.Role == "" && q.MailboxID == 0 && !q.Flagged {
			q.Role = store.Inbox
		}
		var l []store.Message
		call("list", q, &l)
		printList(l)
	case "read":
		fs := flag.NewFlagSet("read", flag.ExitOnError)
		images := fs.Bool("images", false, "load remote images")
		id, rest := firstArg(args)
		fs.Parse(rest)
		var v engine.View
		var m store.Message
		v.Message = &m
		call("open", map[string]any{"id": atoiS(id), "mark_seen": true, "load_images": *images}, &v)
		printView(&v)
	case "flag", "unflag", "unread":
		flagName, on := "flagged", cmd == "flag"
		if cmd == "unread" {
			flagName, on = "seen", false
		}
		call("set_flag", map[string]any{"id": atoi(args, 0), "flag": flagName, "on": on}, nil)
	case "archive", "delete", "junk":
		call(cmd, map[string]int64{"id": atoi(args, 0)}, nil)
	case "save":
		dir := ""
		if len(args) > 2 {
			dir = args[2]
		}
		var r map[string]string
		call("save_attachment", map[string]any{"id": atoi(args, 0), "index": atoi(args, 1), "dir": dir}, &r)
		fmt.Println(r["path"])
	case "send":
		fs := flag.NewFlagSet("send", flag.ExitOnError)
		from := fs.Int64("from", 0, "account id")
		to := fs.String("to", "", "recipients, comma separated")
		cc := fs.String("cc", "", "")
		bcc := fs.String("bcc", "", "")
		subject := fs.String("subject", "", "")
		var attach multi
		fs.Var(&attach, "attach", "file to attach (repeatable)")
		fs.Parse(args)
		body, _ := io.ReadAll(os.Stdin)
		d := msg.Draft{To: addrs(*to), Cc: addrs(*cc), Bcc: addrs(*bcc), Subject: *subject, Text: string(body)}
		call("send", map[string]any{"account_id": *from, "draft": d, "attach_paths": attach}, nil)
		fmt.Println("Sent.")
	case "reply":
		fs := flag.NewFlagSet("reply", flag.ExitOnError)
		all := fs.Bool("all", false, "reply all")
		id, rest := firstArg(args)
		fs.Parse(rest)
		op := "reply"
		if *all {
			op = "reply_all"
		}
		var r struct {
			AccountID int64     `json:"account_id"`
			Draft     msg.Draft `json:"draft"`
		}
		call(op, map[string]int64{"id": atoiS(id)}, &r)
		body, _ := io.ReadAll(os.Stdin)
		r.Draft.Text = string(body) + r.Draft.Text
		call("send", map[string]any{"account_id": r.AccountID, "draft": r.Draft}, nil)
		fmt.Println("Sent.")
	case "mailboxes":
		var l []store.Mailbox
		call("mailboxes", map[string]int64{"account_id": atoiOpt(args)}, &l)
		tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tACCOUNT\tNAME\tROLE\tUNREAD\tTOTAL")
		for _, m := range l {
			fmt.Fprintf(tw, "%d\t%d\t%s\t%s\t%d\t%d\n", m.ID, m.AccountID, m.Path, m.Role, m.Unread, m.Total)
		}
		tw.Flush()
	case "sync":
		call("sync", map[string]int64{"account_id": atoiOpt(args)}, nil)
	case "watch":
		call("subscribe", nil, nil)
		for ev := range c.Events {
			fmt.Println(time.Now().Format("15:04:05"), string(ev))
		}
	default:
		fail(fmt.Errorf("unknown command %q (laser help)", cmd))
	}
}

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

func firstArg(args []string) (string, []string) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args[0], args[1:]
	}
	return "", args
}

func addrs(s string) []msg.Address {
	var out []msg.Address
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, msg.Address{Email: p})
		}
	}
	return out
}

func atoiS(s string) int64 {
	var n int64
	if _, err := fmt.Sscan(s, &n); err != nil {
		fail(fmt.Errorf("expected a number, got %q", s))
	}
	return n
}

func atoi(args []string, i int) int64 {
	if len(args) <= i {
		fail(fmt.Errorf("missing argument (laser help)"))
	}
	return atoiS(args[i])
}

func atoiOpt(args []string) int64 {
	if len(args) == 0 {
		return 0
	}
	return atoiS(args[0])
}

func printList(l []store.Message) {
	tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	for _, m := range l {
		mark := " "
		if !m.Seen {
			mark = "●"
		}
		if m.Flagged {
			mark += "⚑"
		} else {
			mark += " "
		}
		from := m.From.Name
		if from == "" {
			from = m.From.Email
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", m.ID, mark, m.Date.Local().Format("Jan 02 15:04"), trunc(from, 24), trunc(m.Subject, 60))
	}
	tw.Flush()
}

func printView(v *engine.View) {
	fmt.Printf("From:    %s\n", v.From.String())
	fmt.Printf("To:      %s\n", list(v.To))
	if len(v.Cc) > 0 {
		fmt.Printf("Cc:      %s\n", list(v.Cc))
	}
	fmt.Printf("Date:    %s\nSubject: %s\n", v.Date.Local().Format(time.RFC1123), v.Subject)
	for _, a := range v.Attachments {
		if !a.Inline {
			fmt.Printf("Attach:  [%d] %s (%s, %d bytes)\n", a.Index, a.Name, a.Type, a.Size)
		}
	}
	if n := len(v.Privacy.Trackers); n > 0 || v.Privacy.RemoteBlocked > 0 {
		fmt.Printf("Privacy: blocked %d tracker(s) %v, %d remote image(s); cleaned %d link(s)\n", n, v.Privacy.Trackers, v.Privacy.RemoteBlocked, v.Privacy.LinksCleaned)
	}
	fmt.Println()
	fmt.Println(textOf(v.HTML))
}

func list(l []msg.Address) string {
	var s []string
	for _, a := range l {
		s = append(s, a.String())
	}
	return strings.Join(s, ", ")
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func textOf(h string) string {
	p := &msg.Parsed{HTML: strings.NewReplacer("<br>", "\n", "</p>", "\n\n", "</div>", "\n", "</tr>", "\n").Replace(h)}
	return strings.TrimSpace(p.PlainText())
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "laser:", err)
	os.Exit(1)
}
