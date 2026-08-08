// Command awake keeps a computer awake for bounded, observable,
// user-controlled sessions.
//
// This file is the composition root: the only place that knows which concrete
// implementations are in play, where the user's home directory is, and that
// signals exist. Everything it builds is handed to the CLI, which is a thin
// client over the application core (ADR-0001).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/javiervargas02/awake/internal/app"
	"github.com/javiervargas02/awake/internal/buildinfo"
	"github.com/javiervargas02/awake/internal/cli"
	"github.com/javiervargas02/awake/internal/clock"
	"github.com/javiervargas02/awake/internal/lock"
	"github.com/javiervargas02/awake/internal/logging"
	"github.com/javiervargas02/awake/internal/platform"
	"github.com/javiervargas02/awake/internal/store"
)

func main() {
	ctx, stopSignals := watchSignals(context.Background())
	defer stopSignals()

	os.Exit(cli.Run(ctx, os.Args[1:], cli.Deps{
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		Interactive: isTerminal(os.Stdout),
		Version:     buildinfo.Get(),
		NewService:  newService,
	}))
}

// watchSignals turns interrupts into cancellation carrying a domain cause.
//
// Signal handling belongs here, at the process boundary: the core learns
// "stopped by the user" or "interrupted" and never learns that signals exist.
//
// A second signal is deliberately not handled. Notification stops after the
// first, so a second Ctrl-C gets the default behaviour and terminates the
// process immediately — and that is safe precisely because of ADR-0006: the
// keep-awake mechanism dies with its parent whether or not our cleanup runs.
func watchSignals(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(parent)

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		received, ok := <-signals
		if !ok {
			return
		}
		signal.Stop(signals)

		// SIGTERM is what `awake stop` sends; SIGINT is Ctrl-C. The end reason
		// is an interpretation, and the session trace records the raw fact.
		if received == syscall.SIGINT {
			cancel(app.ErrInterrupted)
			return
		}
		cancel(app.ErrStopRequested)
	}()

	return ctx, func() {
		signal.Stop(signals)
		close(signals)
		cancel(context.Canceled)
	}
}

// newService wires the application core.
//
// It is called only by commands that touch state, so `awake version` and
// `awake --help` create nothing on disk.
func newService(verbose bool) (*app.Service, func(), error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, fmt.Errorf("locating your home directory: %w", err)
	}

	st := store.New(filepath.Join(home, ".awake"))
	if err := st.EnsureDirs(); err != nil {
		return nil, nil, fmt.Errorf("preparing %s: %w", st.Root(), err)
	}

	logOptions := logging.Options{
		Clock:  clock.System{},
		Stderr: os.Stderr,
	}
	if verbose {
		logOptions.Verbose = os.Stderr
	}

	cleanup := func() {}
	if file, err := logging.OpenFile(filepath.Join(st.LogDir(), "awake.jsonl")); err == nil {
		logOptions.Global = file
		cleanup = func() { _ = file.Close() }
	} else {
		// Logging must never be the reason a command does not run.
		fmt.Fprintf(os.Stderr, "awake: cannot write the log: %v (continuing)\n", err)
	}

	service := app.New(app.Deps{
		Clock:      clock.System{},
		Store:      st,
		Logger:     logging.New(logOptions),
		Platform:   platform.New(),
		Lock:       lock.New(""),
		AppVersion: buildinfo.Get().Version,
	})

	return service, cleanup, nil
}

// isTerminal reports whether a file is a character device, which is how the
// standard library lets us tell a terminal from a pipe without a dependency.
func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
