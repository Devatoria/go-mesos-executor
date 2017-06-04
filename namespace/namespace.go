package namespace

import (
	"fmt"
	"runtime"

	"github.com/Devatoria/go-mesos-executor/logger"

	"github.com/spf13/viper"
	"github.com/vishvananda/netns"
	"go.uber.org/zap"
)

var current netns.NsHandle // Executor network namespace

func init() {
	var err error
	current, err = netns.Get()
	if err != nil {
		logger.GetInstance().Production.Fatal("Error while retrieving current namespace",
			zap.Error(err),
		)
	}
}

// EnterNetworkNamespace enters the network namespace of the given process
// Do not forget to defer ExitNetworkNamespace in order to unlock thread
func EnterNetworkNamespace(pid int) error {
	path := fmt.Sprintf("%s/%d/ns/net", viper.Get("proc_path"), pid)

	runtime.LockOSThread() // Lock thread to avoid namespace switching
	ns, err := netns.GetFromPath(path)
	if err != nil {
		return err
	}

	if ns.UniqueId() == current.UniqueId() {
		return fmt.Errorf("Process network namespace and current network namespace are the same (ns: %s)", ns.UniqueId())
	}

	logger.GetInstance().Development.Debug("Entering process network namespace",
		zap.String("ns", ns.UniqueId()),
		zap.Int("pid", pid),
	)

	return netns.Set(ns)
}

// ExitNetworkNamespace returns into the executor network namespace
func ExitNetworkNamespace() error {
	defer runtime.UnlockOSThread() // Do not forget to unlock thread

	logger.GetInstance().Development.Debug("Exiting process network namespace",
		zap.String("ns", current.UniqueId()),
	)

	return netns.Set(current)
}
