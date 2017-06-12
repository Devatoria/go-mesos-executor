package logger

import (
	"log"
	"sync"

	"github.com/spf13/viper"

	"go.uber.org/zap"
)

var instance *zap.Logger
var once sync.Once

// GetInstance initializes a logger instance (if needed) and returns it
func GetInstance() *zap.Logger {
	once.Do(func() {
		prodConfig := zap.NewProductionConfig()
		prodConfig.DisableStacktrace = true
		if viper.GetBool("debug") {
			prodConfig.Level.SetLevel(zap.DebugLevel) // Enable debug mode if set in config
		}

		prod, err := prodConfig.Build()
		if err != nil {
			log.Fatalf("Error while initializing production logger: %v", err)
		}

		instance = prod
	})

	return instance
}
