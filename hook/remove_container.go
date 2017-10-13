package hook

import (
	"github.com/Devatoria/go-mesos-executor/container"
	"github.com/Devatoria/go-mesos-executor/logger"
	"github.com/Devatoria/go-mesos-executor/types"

	"go.uber.org/zap"
)

// RemoveContainerHook removes the stopped container on post-stop
var RemoveContainerHook = Hook{
	Name:     "removeContainer",
	Priority: 0,
	RunPostStop: func(c container.Containerizer, info *types.ContainerTaskInfo) error {
		logger.GetInstance().Info("Removing container",
			zap.String("containerID", info.ContainerID),
		)

		return c.ContainerRemove(info.ContainerID)
	},
}
