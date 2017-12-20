package hook

import (
	"github.com/Devatoria/go-mesos-executor/container"
	"github.com/Devatoria/go-mesos-executor/logger"

	"github.com/mesos/mesos-go/api/v1/lib"
	"go.uber.org/zap"
)

// RemoveContainerHook removes the stopped container on post-stop
var RemoveContainerHook = Hook{
	Name:     "removeContainer",
	Priority: 0,
	RunPostStop: func(c container.Containerizer, taskInfo *mesos.TaskInfo, frameworkInfo *mesos.FrameworkInfo, containerID string) error {
		logger.GetInstance().Info("Removing container",
			zap.String("containerID", containerID),
		)

		return c.ContainerRemove(containerID)
	},
}
