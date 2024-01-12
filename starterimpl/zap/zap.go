package zap

import (
	"fmt"

	"github.com/arobie1992/go-clarinet/v2/log"
	"go.uber.org/zap"
	zl "go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type ZapLogger struct {
	zapLogger *zl.SugaredLogger
	level     log.Level
}

func toZapLevel(level log.Level) (zl.AtomicLevel, error) {
	var zapLevel zapcore.Level
	switch level {
	case log.Trace():
		zapLevel = zapcore.DebugLevel
	case log.Debug():
		zapLevel = zapcore.DebugLevel
	case log.Info():
		zapLevel = zapcore.InfoLevel
	case log.Warn():
		zapLevel = zapcore.WarnLevel
	case log.Error():
		zapLevel = zapcore.ErrorLevel
	default:
		return zl.AtomicLevel{}, fmt.Errorf("Unrecognized log level: %s", level)
	}
	return zl.NewAtomicLevelAt(zapLevel), nil
}

func NewZapLogger(level log.Level) (log.Logger, error) {
	zapLevel, err := toZapLevel(level)
	if err != nil {
		return nil, err
	}

	config := zl.NewDevelopmentConfig()
	config.Level = zapLevel
	zapLogger, err := config.Build()
	if err != nil {
		return nil, err
	}
	// ignore the wrapper log interface
	zapLogger = zapLogger.WithOptions(zap.AddCallerSkip(1))

	return &ZapLogger{zapLogger.Sugar(), level}, nil
}

func (l *ZapLogger) Trace(fmtMsg string, values ...any) {
	if l.level.AtLeast(log.Trace()) {
		msg := fmt.Sprintf(fmtMsg, values...)
		l.zapLogger.Debugf("%s\t%s", log.Trace(), msg)
	}
}

func (l *ZapLogger) Debug(fmtMsg string, values ...any) {
	l.zapLogger.Debugf(fmtMsg, values...)
}

func (l *ZapLogger) Info(fmtMsg string, values ...any) {
	l.zapLogger.Infof(fmtMsg, values...)
}

func (l *ZapLogger) Warn(fmtMsg string, values ...any) {
	l.zapLogger.Warnf(fmtMsg, values...)
}

func (l *ZapLogger) Error(fmtMsg string, values ...any) {
	l.zapLogger.Errorf(fmtMsg, values...)
}

func (l *ZapLogger) Level() log.Level {
	return l.level
}