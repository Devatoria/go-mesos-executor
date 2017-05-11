package container

import (
	"github.com/mesos/mesos-go/api/v1/lib"
)

// Containerizer represents a containerizing technology such as docker
type Containerizer interface {
	ContainerRun(info Info) (string, error)
	ContainerStop(id string) error
}

// Info represents container information such as image name, CPU/memory limits...
type Info struct {
	CPUSharesLimit uint64
	Image          string
	MemoryLimit    uint64
	NetworkMode    mesos.ContainerInfo_DockerInfo_Network
	Privileged     bool
}
