package log

import (
	"fmt"
	"strings"
)

type Logger interface {
	Trace(fmtMsg string, values ...any)
	Debug(fmtMsg string, values ...any)
	Info(fmtMsg string, values ...any)
	Warn(fmtMsg string, values ...any)
	Error(fmtMsg string, values ...any)
}

type Level interface {
	logLevel()
	value() int
	AtLeast(level Level) bool
	String() string
}

type traceLevel struct{ val int }
type debugLevel struct{ val int }
type infoLevel struct{ val int }
type warnLevel struct{ val int }
type errorLevel struct{ val int }

func (_ *traceLevel) logLevel() {}
func (_ *debugLevel) logLevel() {}
func (_ *infoLevel) logLevel()  {}
func (_ *warnLevel) logLevel()  {}
func (_ *errorLevel) logLevel() {}

func (t *traceLevel) value() int { return t.val }
func (d *debugLevel) value() int { return d.val }
func (i *infoLevel) value() int  { return i.val }
func (w *warnLevel) value() int  { return w.val }
func (e *errorLevel) value() int { return e.val }

func (t *traceLevel) AtLeast(level Level) bool {
	return t.value() >= level.value()
}
func (d *debugLevel) AtLeast(level Level) bool {
	return d.value() >= level.value()
}
func (i *infoLevel) AtLeast(level Level) bool {
	return i.value() >= level.value()
}
func (w *warnLevel) AtLeast(level Level) bool {
	return w.value() >= level.value()
}
func (e *errorLevel) AtLeast(level Level) bool {
	return e.value() >= level.value()
}

func (_ *traceLevel) String() string { return "TRACE" }
func (_ *debugLevel) String() string { return "DEBUG" }
func (_ *infoLevel) String() string  { return "INFO" }
func (_ *warnLevel) String() string  { return "WARN" }
func (_ *errorLevel) String() string { return "ERROR" }

var tl Level = &traceLevel{5}
var dl Level = &debugLevel{4}
var il Level = &infoLevel{3}
var wl Level = &warnLevel{2}
var el Level = &errorLevel{1}
var levels = []string{tl.String(), dl.String(), il.String(), wl.String(), el.String()}

func Trace() Level { return tl }
func Debug() Level { return dl }
func Info() Level  { return il }
func Warn() Level  { return wl }
func Error() Level { return el }

func ParseLevel(str string) (Level, error) {
	switch strings.ToLower(str) {
	case "trace":
		return Trace(), nil
	case "debug":
		return Debug(), nil
	case "info":
		return Info(), nil
	case "warn":
		return Warn(), nil
	case "error":
		return Error(), nil
	default:
		return nil, fmt.Errorf("Unrecognized log level: %s. Recognized levels are case insensitive %v", str, levels)
	}
}
