package hook

import (
	"github.com/Devatoria/go-mesos-executor/logger"
	"github.com/Devatoria/go-mesos-executor/types"

	"go.uber.org/zap"
)

// LogSomethingHook logs something on pre-run-
var LogSomethingHook = Hook{
	Name:     "logSomething",
	Priority: 0,
	Execute: func(info *types.ContainerTaskInfo) error {
		logger.GetInstance().Production.Info("I'm logging!",
			zap.String("containerID", info.ContainerID),
		)

		return nil
	},
}
