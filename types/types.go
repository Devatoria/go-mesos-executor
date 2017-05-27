package types

import (
	"github.com/mesos/mesos-go/api/v1/lib"
)

// ContainerTaskInfo represents a container linked to a task
// This struct is used to store executor tasks with associated containers
type ContainerTaskInfo struct {
	ContainerID string
	TaskInfo    mesos.TaskInfo
}
