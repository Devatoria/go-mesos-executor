package types

import (
	"context"
	"net"

	"github.com/Devatoria/go-mesos-executor/container"
)

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

// ContainerGetIPsByInterface returns a fake IP
func (f *FakeContainerizer) ContainerGetIPsByInterface(id string, interfaceName string) ([]net.IP, error) {
	return []net.IP{net.ParseIP("127.0.0.1")}, nil
}

// ContainerGetGatewayIP returns a fake IP
func (f *FakeContainerizer) ContainerGetGatewayIP(id string) (net.IP, error) {
	return net.ParseIP("127.0.0.1"), nil
}

// ContainerExec returns a chan with a goroutine injecting a nil error
func (f *FakeContainerizer) ContainerExec(ctx context.Context, id string, cmd []string) (result chan error) {
	ch := make(chan error)
	go func() {
		ch <- nil
	}()

	return ch
}

// ContainerWait returns the 0 exit code with no error
func (f *FakeContainerizer) ContainerWait(id string) (code int, err error) {
	return 0, nil
}
