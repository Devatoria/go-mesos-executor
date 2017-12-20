package hook

import (
	"github.com/Devatoria/go-mesos-executor/container"

	"github.com/mesos/mesos-go/api/v1/lib"
)

// NetworkHook ensure that containers are launched in a certain network.
var NetworkHook = Hook{
	Name:     "network",
	Priority: 0,
	RunPreCreate: func(c container.Containerizer, taskInfo *mesos.TaskInfo, frameworkInfo *mesos.FrameworkInfo) error {
		// Retrieve framework name
		frameworkName := frameworkInfo.GetName()

		// Force the network mode to user mode
		taskInfo.Container.Docker.Network = mesos.ContainerInfo_DockerInfo_USER.Enum()

		// Set the name of the network to use
		taskInfo.Container.NetworkInfos = []mesos.NetworkInfo{
			mesos.NetworkInfo{
				Name: &frameworkName,
			},
		}

		return nil
	},
}
