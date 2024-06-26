package log

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/go-stack/stack"

	"github.com/peterlabuschagne/jettison/errors"
	"github.com/peterlabuschagne/jettison/internal"
	"github.com/peterlabuschagne/jettison/trace"
)

const (
	LevelInfo  Level = "info"
	LevelError Level = "error"
	LevelDebug Level = "debug"
)

type logOption func(*Entry)

func (o logOption) ApplyToLog(e *Entry) {
	o(e)
}

// WithLevel returns a jettison option to override the default log level.
// It only works when provided as option to log package functions.
func WithLevel(level Level) Option {
	return logOption(func(e *Entry) {
		e.Level = level
	})
}

// WithError returns a jettison option to add a structured error as part of
// Info logging. See Error for more details. It only works when provided
// as option to log package functions. Using this option while Error logging
// is not recommended.
func WithError(err error) Option {
	return logOption(func(e *Entry) {
		// Add the most recent error code in the chain to the log's root.
		codes := errors.GetCodes(err)
		if len(codes) > 0 {
			e.ErrorCode = &codes[0]
		}
		addErrors(e, err)
	})
}

type Option interface {
	ApplyToLog(*Entry)
}

func Debug(ctx context.Context, msg string, opts ...Option) {
	logger.Log(ctx, makeEntry(ctx, msg, LevelDebug, opts...))
}

// Info writes a structured jettison log to the logger. Any jettison
// key/value pairs contained in the given context are included in the log.
func Info(ctx context.Context, msg string, opts ...Option) {
	logger.Log(ctx, makeEntry(ctx, msg, LevelInfo, opts...))
}

// Error writes a structured jettison log of the given error to the logger.
// If the error is not already a Jettison error, it is converted into one and
// then logged. Any jettison key/value pairs contained in the given context are
// included in the log.
// If err is nil, a new error is created.
func Error(ctx context.Context, err error, opts ...Option) {
	if err == nil {
		err = errors.New("nil error logged - this is probably a bug")
	}
	opts = append(opts, WithError(err))
	e := makeEntry(ctx, err.Error(), LevelError, opts...)
	logger.Log(ctx, e)
}

func makeEntry(ctx context.Context, msg string, lvl Level, opts ...Option) Entry {
	l := newEntry(msg, lvl, 3)
	for _, o := range opts {
		o.ApplyToLog(&l)
	}
	l.Parameters = append(l.Parameters, ContextKeyValues(ctx)...)

	// Sort the parameters for consistent logging.
	sort.Slice(l.Parameters, func(i, j int) bool {
		return l.Parameters[i].Key < l.Parameters[j].Key
	})

	return l
}

func addErrors(e *Entry, err error) {
	if err == nil {
		return
	}
	paths := errors.Flatten(err)
	if len(paths) == 1 {
		ent := errorEntry(paths[0])
		e.Parameters = append(e.Parameters, ent.Parameters...)
		e.ErrorObject = &ent
	} else {
		for _, p := range paths {
			ent := errorEntry(p)
			e.Parameters = append(e.Parameters, ent.Parameters...)
			e.ErrorObjects = append(e.ErrorObjects, ent)
		}
	}
}

func errorEntry(errPath []error) ErrorObject {
	if len(errPath) == 0 {
		return ErrorObject{}
	}
	var e ErrorObject

	var m trace.Merge
	for _, err := range errPath {
		// Set the message to the last non-joined message in the path
		// Clear the message if this is a Joined error (i.e. unwraps to multiple errors)
		// We don't need the joined error message because we will iterate over the
		// other paths in turn.
		if _, isJoin := err.(interface{ Unwrap() []error }); isJoin {
			e.Message = ""
		} else if e.Message == "" {
			e.Message = err.Error()
		}

		je, ok := err.(*internal.Error)
		if !ok {
			continue
		}
		// Use the highest non-empty code
		if e.Code == "" {
			e.Code = je.Code
		}
		// Use the lowest non-empty source string
		if je.Source != "" {
			e.Source = je.Source
		}
		if je.Binary != "" {
			e.Stack = append(e.Stack, je.Binary)
		}
		e.Parameters = append(e.Parameters, je.KV...)
		if len(je.StackTrace) > 0 {
			m.Add(je.StackTrace, je.Binary)
		}
	}
	e.StackTrace = MakeElastic(m.FullTrace())
	return e
}

// newEntry returns an Entry struct decorated with useful defaults - stackSkip
// is the number of callstacks to skip in the stacktrace before pulling
// out the `source` of the call to `jettison/log.XXX`.
func newEntry(msg string, level Level, stackSkip int) Entry {
	return Entry{
		Message:   msg,
		Source:    fmt.Sprintf("%+v", stack.Caller(stackSkip)),
		Level:     level,
		Timestamp: time.Now(),
	}
}

type Interface interface {
	Debug(ctx context.Context, msg string, ol ...Option)
	Info(ctx context.Context, msg string, ol ...Option)
	Error(ctx context.Context, err error, ol ...Option)
}

type Jettison struct{}

func (j Jettison) Debug(ctx context.Context, msg string, ol ...Option) {
	Debug(ctx, msg, ol...)
}

func (j Jettison) Info(ctx context.Context, msg string, ol ...Option) {
	Info(ctx, msg, ol...)
}

func (j Jettison) Error(ctx context.Context, err error, ol ...Option) {
	Error(ctx, err, ol...)
}

var _ Interface = (*Jettison)(nil)
