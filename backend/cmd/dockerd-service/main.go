package main

import (
	"context"
	"dockerd.local/backend/internal/api"
	engine "dockerd.local/backend/internal/runtime"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

func main() {
	home, _ := os.UserHomeDir()
	state := flag.String("home", filepath.Join(home, "Library", "Application Support", "DockerD"), "private state directory")
	bundle := flag.String("bundle", ".runtime/bundle", "runtime binary bundle")
	resources := flag.String("resources", "resources", "application resources")
	flag.Parse()
	if e := os.MkdirAll(*state, 0700); e != nil {
		log.Fatal(e)
	}
	if e := os.Chmod(*state, 0700); e != nil {
		log.Fatal(e)
	}
	// A process lock prevents concurrent service startups from unlinking each
	// other's sockets or modifying the same VM.
	lock, e := os.OpenFile(filepath.Join(*state, "service.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		log.Fatal(e)
	}
	defer lock.Close()
	if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		log.Fatal("DockerD service is already running")
	}
	m := engine.New(engine.Paths{Home: *state, Bundle: *bundle, Resources: *resources})
	s, e := api.New(m)
	if e != nil {
		log.Fatal(e)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if e = api.Serve(ctx, s); e != nil {
		log.Fatal(e)
	}
}
