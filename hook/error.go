package hook

import (
	"errors"

	"github.com/Devatoria/go-mesos-executor/container"
	"github.com/mesos/mesos-go/api/v1/lib"
)

type ErrorHook struct {
}

func (h *ErrorHook) GetName() string {
	return "error"
}

func (h *ErrorHook) GetPriority() int64 {
	return 0
}

func (h *ErrorHook) RunPreCreate(containerizer container.Containerizer, taskInfo *mesos.TaskInfo) error {
	return errors.New("an error")
}

func (h *ErrorHook) RunPreRun(containerizer container.Containerizer, taskInfo *mesos.TaskInfo, containerID string) error {
	return errors.New("an error")
}

func (h *ErrorHook) RunPostRun(containerizer container.Containerizer, taskInfo *mesos.TaskInfo, containerID string) error {
	return errors.New("an error")
}

func (h *ErrorHook) RunPreStop(containerizer container.Containerizer, taskInfo *mesos.TaskInfo, containerID string) error {
	return errors.New("an error")
}

func (h *ErrorHook) RunPostStop(containerizer container.Containerizer, taskInfo *mesos.TaskInfo, containerID string) error {
	return errors.New("an error")
}
