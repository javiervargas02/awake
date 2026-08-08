package logging

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/javiervargas02/awake/internal/clock"
)

// Options configures the global logger. Writers are injected rather than
// opened here, so the composition root stays the only place that knows where
// files live, and tests need no filesystem.
type Options struct {
	Clock clock.Clock

	// Global receives every event. Nil disables the global log without
	// disabling logging.
	Global io.Writer

	// Verbose mirrors events to a human-readable stream, normally stderr.
	// It changes where events are echoed, never what is logged.
	Verbose io.Writer

	// Stderr receives the one-off notice emitted if logging itself breaks.
	Stderr io.Writer
}

// core is the state shared by the global logger and every session logger
// derived from it. Sharing one mutex is what keeps concurrent writers from
// interleaving, and sharing the degradation flags is what keeps a broken sink
// from producing one complaint per event.
type core struct {
	mu      sync.Mutex
	clock   clock.Clock
	global  io.Writer
	verbose io.Writer
	stderr  io.Writer

	globalFailed bool
	noticeGiven  bool
}

// Logger writes structured events.
//
// A Logger is either global or bound to a session. Session-bound loggers carry
// the session ID and a trace writer, so no caller has to remember to attach
// either.
type Logger struct {
	core      *core
	sessionID string
	trace     io.Writer
}

func New(opts Options) *Logger {
	c := opts.Clock
	if c == nil {
		c = clock.System{}
	}
	return &Logger{core: &core{
		clock:   c,
		global:  opts.Global,
		verbose: opts.Verbose,
		stderr:  opts.Stderr,
	}}
}

// WithSession returns a logger bound to a session. Events emitted through it go
// to the session's own trace and to the global log: the global log stays a
// complete timeline, and the trace stays independently readable and shareable.
func (l *Logger) WithSession(sessionID string, trace io.Writer) *Logger {
	return &Logger{core: l.core, sessionID: sessionID, trace: trace}
}

// SessionID reports which session this logger is bound to, if any.
func (l *Logger) SessionID() string { return l.sessionID }

func (l *Logger) Info(event string, data Fields)  { l.Log(LevelInfo, event, data) }
func (l *Logger) Warn(event string, data Fields)  { l.Log(LevelWarn, event, data) }
func (l *Logger) Error(event string, data Fields) { l.Log(LevelError, event, data) }

// Log writes one event.
//
// It never returns an error and never panics. Logging must not be the reason a
// session ends: a session that keeps its promise with degraded logging is
// better than one that abandons the user's machine because a file was
// unwritable.
func (l *Logger) Log(level Level, event string, data Fields) {
	c := l.core

	c.mu.Lock()
	defer c.mu.Unlock()

	line, err := newEnvelope(c.clock.Now(), level, event, l.sessionID, data).encode()
	if err != nil {
		c.notice(fmt.Sprintf("could not encode event %q: %v", event, err))
		return
	}

	// The session trace first: if it fails, the global log is where that fact
	// is recorded, and it is still open at this point.
	if l.trace != nil {
		if _, writeErr := l.trace.Write(line); writeErr != nil {
			l.trace = nil
			c.writeGlobal(l.sessionID, EventLogSinkFailed, Fields{
				"sink":  "session_trace",
				"error": writeErr.Error(),
			})
		}
	}

	c.writeLine(line)
	c.echo(level, event, l.sessionID, data)
}

// writeGlobal emits an event to the global log only, without re-entering Log.
// It exists so that reporting a broken sink cannot recurse through the sink
// that just broke.
func (c *core) writeGlobal(sessionID, event string, data Fields) {
	line, err := newEnvelope(c.clock.Now(), LevelWarn, event, sessionID, data).encode()
	if err != nil {
		return
	}
	c.writeLine(line)
}

func (c *core) writeLine(line []byte) {
	if c.global == nil || c.globalFailed {
		return
	}
	if _, err := c.global.Write(line); err != nil {
		// Degrade once. A broken global log must not produce one complaint per
		// event for the rest of the session.
		c.globalFailed = true
		c.notice(fmt.Sprintf("log writes are failing: %v", err))
	}
}

func (c *core) notice(message string) {
	if c.stderr == nil || c.noticeGiven {
		return
	}
	c.noticeGiven = true
	fmt.Fprintf(c.stderr, "awake: %s (the session is unaffected)\n", message)
}

// echo renders an event for a human watching --verbose. It is a window onto
// the log files, not a second log: the files are identical with and without it.
func (c *core) echo(level Level, event, sessionID string, data Fields) {
	if c.verbose == nil {
		return
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s %-5s %-24s", c.clock.Now().UTC().Format("15:04:05.000000"), level, event)
	if sessionID != "" {
		fmt.Fprintf(&b, " session=%s", sessionID)
	}
	for _, key := range sortedKeys(data) {
		fmt.Fprintf(&b, " %s=%v", key, data[key])
	}
	b.WriteByte('\n')

	fmt.Fprint(c.verbose, b.String())
}

func sortedKeys(data Fields) []string {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
