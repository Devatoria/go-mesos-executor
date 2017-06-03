package types

import (
	"net"

	"github.com/Devatoria/go-mesos-executor/container"

	"github.com/mesos/mesos-go/api/v1/lib"
)

// ContainerTaskInfo represents a container linked to a task
// This struct is used to store executor tasks with associated containers
type ContainerTaskInfo struct {
	ContainerID string
	TaskInfo    mesos.TaskInfo
}

// FakeContainerizer is a fake containerizer that always succeed
type FakeContainerizer struct{}

// NewFakeContainerizer returns a fake containerizer instance
func NewFakeContainerizer() *FakeContainerizer {
	return &FakeContainerizer{}
}

// ContainerCreate returns a fake ID
func (f *FakeContainerizer) ContainerCreate(container.Info) (string, error) {
	return "fakeContainerID", nil
}

// ContainerRun returns nil
func (f *FakeContainerizer) ContainerRun(id string) error {
	return nil
}

// ContainerStop returns nil
func (f *FakeContainerizer) ContainerStop(id string) error {
	return nil
}

// ContainerRemove returns nil
func (f *FakeContainerizer) ContainerRemove(id string) error {
	return nil
}

// ContainerGetPID returns a fake PID
func (f *FakeContainerizer) ContainerGetPID(id string) (int, error) {
	return 666, nil
}

// ContainerGetGatewayIP returns a fake IP
func (f *FakeContainerizer) ContainerGetGatewayIP(id string) (net.IP, error) {
	return net.ParseIP("127.0.0.1"), nil
}
