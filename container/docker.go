package container

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Sirupsen/logrus"
	"github.com/fsouza/go-dockerclient"
	"github.com/mesos/mesos-go/api/v1/lib"
)

// DockerContainerizer represents a docker containerizer
type DockerContainerizer struct {
	Client *docker.Client
}

// NewDockerContainerizer initializes a new docker containerizer
func NewDockerContainerizer(socket string) (*DockerContainerizer, error) {
	// If socket is given without an explicit protocol such as tpc:// or http://,
	// we use unix:// one
	if strings.HasPrefix(socket, "/") {
		socket = "unix://" + socket
	}

	client, err := docker.NewClient(socket)
	if err != nil {
		return nil, err
	}

	return &DockerContainerizer{
		Client: client,
	}, nil
}

// ContainerRun launches a new container with the given containerizer
func (c *DockerContainerizer) ContainerRun(info Info) (string, error) {
	// Define network mode
	var networkMode string
	switch info.TaskInfo.GetContainer().GetDocker().GetNetwork() {
	case mesos.ContainerInfo_DockerInfo_HOST:
		networkMode = "host"
		break
	case mesos.ContainerInfo_DockerInfo_BRIDGE:
		networkMode = "bridge"
		break
	case mesos.ContainerInfo_DockerInfo_NONE:
		networkMode = "none"
		break
	case mesos.ContainerInfo_DockerInfo_USER:
		networkMode = "user"
		break
	}

	// Define ports mappings
	portsMappings := make(map[docker.Port][]docker.PortBinding)
	for _, mapping := range info.TaskInfo.GetContainer().GetDocker().GetPortMappings() {
		containerPort := docker.Port(fmt.Sprintf("%d/%s", mapping.GetContainerPort(), mapping.GetProtocol())) // ContainerPort needs to have the form port/protocol (eg. 80/tcp)
		hostPort := strconv.Itoa(int(mapping.HostPort))
		portsMappings[containerPort] = []docker.PortBinding{
			docker.PortBinding{
				HostPort: hostPort,
			},
		}
	}

	// Prepare container
	container, err := c.Client.CreateContainer(docker.CreateContainerOptions{
		Config: &docker.Config{
			CPUShares: int64(info.CPUSharesLimit),
			Image:     info.TaskInfo.GetContainer().GetDocker().GetImage(),
			Memory:    int64(info.MemoryLimit),
		},
		HostConfig: &docker.HostConfig{
			NetworkMode:  networkMode,
			PortBindings: portsMappings,
			Privileged:   info.TaskInfo.GetContainer().GetDocker().GetPrivileged(),
		},
	})

	if err != nil {
		return "", err
	}

	// Start the container
	logrus.WithFields(logrus.Fields{
		"NetworkMode":   networkMode,
		"PortsMappings": portsMappings,
	}).Debug("Starting docker container")
	err = c.Client.StartContainer(container.ID, nil)
	if err != nil {
		return "", err
	}

	return container.ID, nil
}

// ContainerStop stops the given container
func (c *DockerContainerizer) ContainerStop(id string) error {
	return c.Client.StopContainer(id, 0)
}
