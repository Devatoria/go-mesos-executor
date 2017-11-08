package hook

import (
	"github.com/Devatoria/go-mesos-executor/container"
	"github.com/mesos/mesos-go/api/v1/lib"
)

type SampleHook struct {
}

func (h *SampleHook) GetName() string {
	return "sample"
}

func (h *SampleHook) GetPriority() int64 {
	return 0
}

func (h *SampleHook) RunPreCreate(containerizer container.Containerizer, taskInfo *mesos.TaskInfo) error {
	return nil
}

func (h *SampleHook) RunPreRun(containerizer container.Containerizer, taskInfo *mesos.TaskInfo, containerID string) error {
	return nil
}

func (h *SampleHook) RunPostRun(containerizer container.Containerizer, taskInfo *mesos.TaskInfo, containerID string) error {
	return nil
}

func (h *SampleHook) RunPreStop(containerizer container.Containerizer, taskInfo *mesos.TaskInfo, containerID string) error {
	return nil
}

func (h *SampleHook) RunPostStop(containerizer container.Containerizer, taskInfo *mesos.TaskInfo, containerID string) error {
	return nil
}
