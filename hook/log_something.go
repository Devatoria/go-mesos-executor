package hook

import (
	"github.com/Devatoria/go-mesos-executor/logger"
)

// LogSomethingHook logs something on pre-create
var LogSomethingHook = Hook{
	Name:     "logSomething",
	Priority: 0,
	Execute: func() error {
		logger.GetInstance().Production.Info("I'm logging!")

		return nil
	},
}
