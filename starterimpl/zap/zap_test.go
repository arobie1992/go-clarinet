package zap_test

import (
	"testing"

	"github.com/arobie1992/go-clarinet/starterimpl/zap"
	"github.com/arobie1992/go-clarinet/v2/log"
)

func TestNewZapLogger(t *testing.T) {
	tests := []log.Level{
		log.Trace(),
		log.Debug(),
		log.Info(),
		log.Warn(),
		log.Error(),
	}
	for i, level := range tests {
		l, err := zap.NewZapLogger(level)
		if err != nil {
			t.Errorf("Test %d encountered error: %s", i, err)
			continue
		}
		if l.Level() != level {
			t.Errorf("Log level did not match. Expected: %s, Got: %s", l.Level(), level)
		}
	}
}
