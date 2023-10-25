package log

import (
	"sync"

	"go.uber.org/zap"
)

var log *zap.SugaredLogger
var once sync.Once

func InitLogger() {
	once.Do(func() {
		logger, _ := zap.NewDevelopment()
		log = logger.Sugar()	
	})
}

func Log() *zap.SugaredLogger {
	return log
}
