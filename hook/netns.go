package hook

import (
	"fmt"
	"os"

	"github.com/Devatoria/go-mesos-executor/container"
	"github.com/spf13/viper"

	"github.com/mesos/mesos-go/api/v1/lib"
)

type NetnsHook struct {
	pid         int
	symlinkPath string
}

func (h *NetnsHook) GetName() string {
	return "netns"
}

func (h *NetnsHook) GetPriority() int64 {
	return 0
}

func (h *NetnsHook) RunPostRun(containerizer container.Containerizer, taskInfo *mesos.TaskInfo, containerID string) error {
	// Get (and store) container PID
	pid, err := containerizer.ContainerGetPID(containerID)
	if err != nil {
		return err
	}
	h.pid = pid

	// Create netns folder if doesn't exist
	if err = os.Mkdir(viper.GetString("netns.path"), 0755); err != nil && !os.IsExist(err) {
		return err
	}

	// Create symlink
	nspath := fmt.Sprintf("%s/%d/ns/net", viper.GetString("proc_path"), h.pid)
	h.symlinkPath = fmt.Sprintf("%s/%s", viper.GetString("netns.path"), taskInfo.TaskID.GetValue())

	return os.Symlink(nspath, h.symlinkPath)
}

func (h *NetnsHook) RunPostStop(containerizer container.Containerizer, taskInfo *mesos.TaskInfo, containerID string) error {
	return os.Remove(h.symlinkPath)
}
