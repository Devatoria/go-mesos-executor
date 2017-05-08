package container

import (
	"strings"

	"github.com/fsouza/go-dockerclient"
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
func (c *DockerContainerizer) ContainerRun(name, image string) error {
	container, err := c.Client.CreateContainer(docker.CreateContainerOptions{
		Name: name,
		Config: &docker.Config{
			Image: image,
		},
	})

	if err != nil {
		return err
	}

	err = c.Client.StartContainer(container.ID, nil)
	return err
}
