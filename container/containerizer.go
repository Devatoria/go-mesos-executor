package container

import (
	"github.com/mesos/mesos-go/api/v1/lib"
)

// Containerizer represents a containerizing technology such as docker
type Containerizer interface {
	ContainerCreate(Info) (string, error)
	ContainerRun(string) error
	ContainerStop(string) error
	ContainerRemove(string) error
}

// Info represents container information such as image name, CPU/memory limits...
type Info struct {
	CPUSharesLimit uint64
	MemoryLimit    uint64
	TaskInfo       mesos.TaskInfo
}
