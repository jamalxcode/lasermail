// laserd is Laser Email's engine: it syncs every account, keeps the local
// store and answers the app over a Unix socket.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jamalxcode/laser-email/internal/browser"
	"github.com/jamalxcode/laser-email/internal/daemon"
	"github.com/jamalxcode/laser-email/internal/engine"
	"github.com/jamalxcode/laser-email/internal/oauth"
	"github.com/jamalxcode/laser-email/internal/paths"
)

// Google OAuth client for "Sign in with Google", embedded by release builds
// (-ldflags "-X main.googleClientID=…"). LASER_GOOGLE_CLIENT_ID overrides.
var (
	googleClientID     string
	googleClientSecret string
)

func main() {
	data := flag.String("data", paths.DataDir(), "data folder")
	sock := flag.String("socket", "", "socket path (default: <data>/laserd.sock)")
	version := flag.Bool("version", false, "print the version")
	demo := flag.Bool("demo", false, "offline demo: made-up accounts and mail, no syncing")
	flag.Parse()
	if *version {
		fmt.Println(daemon.Version)
		return
	}
	if *sock == "" {
		if s := os.Getenv("LASER_SOCKET"); s != "" {
			*sock = s
		} else {
			*sock = *data + "/laserd.sock"
		}
	}
	if v := os.Getenv("LASER_GOOGLE_CLIENT_ID"); v != "" {
		googleClientID, googleClientSecret = v, os.Getenv("LASER_GOOGLE_CLIENT_SECRET")
	}
	if err := os.MkdirAll(*data, 0o700); err != nil {
		log.Fatal(err)
	}
	os.Chmod(*data, 0o700)

	e, err := engine.New(engine.Config{
		DataDir: *data,
		OAuth:   oauth.Config{ClientID: googleClientID, ClientSecret: googleClientSecret},
		OpenURL: browser.Open,
		Offline: *demo,
	})
	if err != nil {
		log.Fatal(err)
	}
	if *demo {
		if err := e.SeedDemo(time.Now()); err != nil {
			log.Fatal(err)
		}
	}
	srv := daemon.Server(e, googleClientID != "")
	if err := srv.Listen(*sock); err != nil {
		e.Close()
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("laserd %s: %s", daemon.Version, *sock)
	if err := srv.Serve(ctx); err != nil {
		log.Print(err)
	}
	e.Close()
	os.Remove(*sock)
}
