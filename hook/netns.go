package hook

import (
	"fmt"
	"os"

	"github.com/Devatoria/go-mesos-executor/container"
	"github.com/spf13/viper"

	"github.com/mesos/mesos-go/api/v1/lib"
)

var (
	netnsHookContainerPID int
	netnsHookSymlinkPath  string
)

// NetnsHook creates and removes a symlink in /var/run/netns in order
// to allow the "ip netns" command to execute commands in associated container
// network namespace (for debug purpose)
var NetnsHook = Hook{
	Name:     "netns",
	Priority: 0,
	RunPostRun: func(c container.Containerizer, taskInfo *mesos.TaskInfo, frameworkInfo *mesos.FrameworkInfo, containerID string) error {
		// Get (and store) container PID
		pid, err := c.ContainerGetPID(containerID)
		if err != nil {
			return err
		}
		netnsHookContainerPID = pid

		// Create netns folder if doesn't exist
		if err = os.Mkdir(viper.GetString("netns.path"), 0755); err != nil && !os.IsExist(err) {
			return err
		}

		// Create symlink
		nspath := fmt.Sprintf("%s/%d/ns/net", viper.GetString("proc_path"), netnsHookContainerPID)
		netnsHookSymlinkPath = fmt.Sprintf("%s/%s", viper.GetString("netns.path"), taskInfo.TaskID.GetValue())

		return os.Symlink(nspath, netnsHookSymlinkPath)
	},
	RunPostStop: func(c container.Containerizer, taskInfo *mesos.TaskInfo, frameworkInfo *mesos.FrameworkInfo, containerID string) error {
		return os.Remove(netnsHookSymlinkPath)
	},
}
