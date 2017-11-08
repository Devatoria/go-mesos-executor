package hook

import (
	"github.com/Devatoria/go-mesos-executor/container"

	"github.com/mesos/mesos-go/api/v1/lib"
)

type RunnableHook interface {
	GetName() string
	GetPriority() int64
}

type SetupableHook interface {
	SetupHook()
}

type PreCreateRunnableHook interface {
	RunPreCreate(containerizer container.Containerizer, taskInfo *mesos.TaskInfo) error
}

type PreRunRunnableHook interface {
	RunPreRun(containerizer container.Containerizer, taskInfo *mesos.TaskInfo, containerID string) error
}

type PostRunRunnableHook interface {
	RunPostRun(containerizer container.Containerizer, taskInfo *mesos.TaskInfo, containerID string) error
}

type PreStopRunnableHook interface {
	RunPreStop(containerizer container.Containerizer, taskInfo *mesos.TaskInfo, containerID string) error
}

type PostStopRunnableHook interface {
	RunPostStop(containerizer container.Containerizer, taskInfo *mesos.TaskInfo, containerID string) error
}
