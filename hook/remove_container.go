package hook

import (
	"github.com/Devatoria/go-mesos-executor/container"
	"github.com/Devatoria/go-mesos-executor/logger"

	"github.com/mesos/mesos-go/api/v1/lib"
	"go.uber.org/zap"
)

type RemoveContainerHook struct{}

func (h *RemoveContainerHook) GetName() string {
	return "removeContainer"
}

func (h *RemoveContainerHook) GetPriority() int64 {
	return 0
}

func (h *RemoveContainerHook) RunPostStop(containerizer container.Containerizer, taskInfo *mesos.TaskInfo, containerID string) error {
	logger.GetInstance().Info("Removing container",
		zap.String("containerID", containerID),
	)

	return containerizer.ContainerRemove(containerID)
}
