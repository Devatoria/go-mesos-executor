package logger

import (
	"sync"

	"go.uber.org/zap"
)

// Logger defines the application logger structure, with development (debug) and production loggers
type Logger struct {
	Development *zap.Logger
	Production  *zap.Logger
}

var instance *Logger
var once sync.Once

// GetInstance returns (and, if needed, initializes) the logger instance
func GetInstance() *Logger {
	once.Do(func() {
		development, _ := zap.NewDevelopment()
		production, _ := zap.NewProduction()

		instance = &Logger{
			Development: development,
			Production:  production,
		}
	})

	return instance
}
