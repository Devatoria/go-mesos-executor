package container

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/Devatoria/go-mesos-executor/logger"
	"github.com/Devatoria/go-mesos-executor/namespace"

	docker "github.com/fsouza/go-dockerclient"
	mesos "github.com/mesos/mesos-go/api/v1/lib"
	"github.com/spf13/viper"

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
		if len(info.TaskInfo.Container.NetworkInfos) > 0 {
			networkMode = *info.TaskInfo.Container.NetworkInfos[0].Name
		} else {
			return "", fmt.Errorf("Could not find a user network to use")
		}
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
		Name: info.Name,
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

// ContainerGetIPsByInterface returns the IPs of the container in the different networks.
// This is done by entering the container networking namespace, and returning the Ips that matches this interface.
func (c *DockerContainerizer) ContainerGetIPsByInterface(id string, hostInterfaceName string) ([]net.IP, error) {
	ips := []net.IP{}

	// Retrieve container informations
	con, err := c.Client.InspectContainer(id)
	if err != nil {
		return ips, err
	}

	// Retrieve the host interface
	hostInterface, err := net.InterfaceByName(hostInterfaceName)
	if err != nil {
		return ips, err
	}

	// Retrieve the host interfaceIP
	hostInterfaceIPs, err := hostInterface.Addrs()
	if err != nil {
		return ips, err
	}

	// Enter container network namespace
	namespace.EnterNetworkNamespace(con.State.Pid)

	// Exit the container network namespace when we are done
	defer namespace.ExitNetworkNamespace()

	// Iterate over the host interface IPs
	for _, hostIPAddr := range hostInterfaceIPs {
		// Retrieve the network corresponding to the host interface ip
		hostIPNet, ok := hostIPAddr.(*net.IPNet)
		if !ok {
			return ips, fmt.Errorf("Could not retrieve the host interface IP")
		}

		// Retrieve all container interfaces
		containerInterfaces, err := net.Interfaces()
		if err != nil {
			return ips, err
		}

		// Iterate over the container interfaces
		for _, ci := range containerInterfaces {
			// Retrieve the corresponding interface IPs
			containerIPs, err := ci.Addrs()
			if err != nil {
				return ips, err
			}

			// Iterate over the interface IPs
			for _, containerIPAddr := range containerIPs {
				// Retrieve container interface ip
				ip, ok := containerIPAddr.(*net.IPNet)
				if !ok {
					return ips, fmt.Errorf("Could not retrieve containers network interface ip")
				}

				// Check if the interface ip match the host interface network
				if hostIPNet.Contains(ip.IP) {
					// Add the container IP to the result if it does match
					ips = append(ips, ip.IP)
				}
			}
		}
	}

	return ips, nil
}

// ContainerExec executes the given command in the given container with the given context
// It does it asynchronously, and returns a channel with an error (or nil if ok)
func (c *DockerContainerizer) ContainerExec(ctx context.Context, id string, cmd []string) chan error {
	result := make(chan error)
	go func(r chan error) {
		logger.GetInstance().Debug("Create a docker exec with options",
			zap.Bool("attachStderr", true),
			zap.Strings("cmd", cmd),
			zap.String("containerID", id),
		)
		// Create the exec instance
		ex, err := c.Client.CreateExec(docker.CreateExecOptions{
			AttachStderr: true,
			Cmd:          cmd,
			Container:    id,
		})
		if err != nil {
			r <- err
			return
		}

		// Start the instance in a goroutine in order to be async
		var stderr bytes.Buffer
		logger.GetInstance().Debug("start dockerExec previously created",
			zap.String("execID", ex.ID),
		)
		err = c.Client.StartExec(ex.ID, docker.StartExecOptions{
			ErrorStream: &stderr,
			Context:     ctx,
		})
		if err != nil {
			r <- err
			return
		}

		// poll docker for exec result
		for {
			select {
			case <-ctx.Done():
				// check one last time if it still running ?
			default:
				time.Sleep(viper.GetDuration("docker_exec_poll_interval"))
				c.checkExec(ex.ID, r)
			}
		}
	}(result)

	return result
}

func (c *DockerContainerizer) checkExec(execID string, returnChan chan error) {
	execResult, err := c.Client.InspectExec(execID)
	if err != nil {
		returnChan <- err
		return
	}
	logger.GetInstance().Debug("Inspect exec",
		zap.String("execID", execResult.ID),
		zap.Int("exitCode", execResult.ExitCode),
		zap.Bool("Running", execResult.Running),
	)
	if !execResult.Running {
		if execResult.ExitCode == 0 {
			// all good, command return zero
			returnChan <- nil
		} else {
			returnChan <- fmt.Errorf("Non zero return code got %d", execResult.ExitCode)
		}
		return
	}
}

// ContainerWait waits for the given container to stop and returns its
// exit code. This function is blocking.
func (c *DockerContainerizer) ContainerWait(id string) (int, error) {
	return c.Client.WaitContainer(id)
}
