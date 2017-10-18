package container

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/Devatoria/go-mesos-executor/logger"

	"github.com/fsouza/go-dockerclient"
	"github.com/mesos/mesos-go/api/v1/lib"
	"go.uber.org/zap"
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

// ContainerCreate creates a new container (but do not start it)
func (c *DockerContainerizer) ContainerCreate(info Info) (string, error) {
	// Define network mode
	var networkMode string
	switch info.TaskInfo.GetContainer().GetDocker().GetNetwork() {
	case mesos.ContainerInfo_DockerInfo_HOST:
		networkMode = "host"
	case mesos.ContainerInfo_DockerInfo_BRIDGE:
		networkMode = "bridge"
	case mesos.ContainerInfo_DockerInfo_NONE:
		networkMode = "none"
	case mesos.ContainerInfo_DockerInfo_USER:
		networkMode = "user"
	default:
		return "", fmt.Errorf("Invalid network mode")
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

	// Define environment variables
	// Docker needs to have a string slice with strings of the form key=val
	var stringifiedEnv []string
	environment := info.TaskInfo.Command.GetEnvironment().GetVariables()
	for _, variable := range environment {
		stringifiedEnv = append(stringifiedEnv, fmt.Sprintf("%s=%s", variable.GetName(), variable.GetValue()))
	}

	// Define volumes
	// Volumes must be passed to "binds" host configuration parameter
	// and have the form: hostPath:containerPath:mode
	var binds []string
	for _, volume := range info.TaskInfo.GetContainer().GetVolumes() {
		var mode string
		switch volume.GetMode() {
		case mesos.RW:
			mode = "rw"
		case mesos.RO:
			mode = "ro"
		default:
			return "", fmt.Errorf("Invalid volume mode: %v", volume.GetMode().String())
		}

		bind := fmt.Sprintf("%s:%s:%s", volume.GetHostPath(), volume.GetContainerPath(), mode)
		binds = append(binds, bind)
	}

	// Create container config
	containerConfig := &docker.Config{
		CPUShares: int64(info.CPUSharesLimit),
		Env:       stringifiedEnv,
		Image:     info.TaskInfo.GetContainer().GetDocker().GetImage(),
		Memory:    int64(info.MemoryLimit),
	}

	// Define cmd
	cmdValue := info.TaskInfo.GetCommand().GetValue()
	if len(cmdValue) > 0 {
		containerConfig.Cmd = strings.Split(cmdValue, " ")
	}

	// Prepare container
	logger.GetInstance().Debug("Creating a new container",
		zap.String("networkMode", networkMode),
		zap.Reflect("portsMappings", portsMappings),
	)
	container, err := c.Client.CreateContainer(docker.CreateContainerOptions{
		Config: containerConfig,
		HostConfig: &docker.HostConfig{
			Binds:        binds,
			NetworkMode:  networkMode,
			PortBindings: portsMappings,
			Privileged:   info.TaskInfo.GetContainer().GetDocker().GetPrivileged(),
		},
	})
	if err != nil {
		return "", err
	}

	return container.ID, nil
}

// ContainerRun launches a new container with the given containerizer
func (c *DockerContainerizer) ContainerRun(id string) error {
	return c.Client.StartContainer(id, nil)
}

// ContainerStop stops the given container
func (c *DockerContainerizer) ContainerStop(id string) error {
	return c.Client.StopContainer(id, 0)
}

// ContainerRemove removes the given container
func (c *DockerContainerizer) ContainerRemove(id string) error {
	return c.Client.RemoveContainer(docker.RemoveContainerOptions{
		ID: id,
	})
}

// ContainerGetPID returns the PID of the given container
func (c *DockerContainerizer) ContainerGetPID(id string) (int, error) {
	con, err := c.Client.InspectContainer(id)
	if err != nil {
		return 0, err
	}

	return con.State.Pid, nil
}

// ContainerGetIPs returns the IPs of the container in the different networks.
// If the container is in host network, the function will return an empty map.
func (c *DockerContainerizer) ContainerGetIPs(id string) (map[string]net.IP, error) {
	ips := make(map[string]net.IP)
	con, err := c.Client.InspectContainer(id)
	if err != nil {
		return ips, err
	}

	for name, conf := range con.NetworkSettings.Networks {
		if name != "host" {
			stringIP := conf.IPAddress
			ip := net.ParseIP(stringIP)
			if ip == nil {
				return ips, fmt.Errorf("Invalid container IP: %s", stringIP)
			}
			ips[name] = ip
		}
	}

	return ips, nil
}

// ContainerExec executes the given command in the given container with the given context
// It does it asynchronously, and returns a channel with an error (or nil if ok)
func (c *DockerContainerizer) ContainerExec(ctx context.Context, id string, cmd []string) chan error {
	result := make(chan error)
	go func(r chan error) {
		// Create the exec instance
		ex, err := c.Client.CreateExec(docker.CreateExecOptions{
			AttachStderr: true,
			Cmd:          cmd,
			Container:    id,
		})
		if err != nil {
			r <- err
		}

		// Start the instance in a goroutine in order to be async
		var stderr bytes.Buffer
		err = c.Client.StartExec(ex.ID, docker.StartExecOptions{
			ErrorStream: &stderr,
			Context:     ctx,
		})

		r <- err
	}(result)

	return result
}

// ContainerWait waits for the given container to stop and returns its
// exit code. This function is blocking.
func (c *DockerContainerizer) ContainerWait(id string) (int, error) {
	return c.Client.WaitContainer(id)
}
