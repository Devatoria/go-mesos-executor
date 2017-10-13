package hook

import (
	"github.com/Devatoria/go-mesos-executor/container"
	"github.com/Devatoria/go-mesos-executor/types"
)

// Hook represents an executable hook (we don't care if it's a pre-create, post-stop or whatever)
type Hook struct {
	Name         string
	Priority     int64
	RunPreCreate func(container.Containerizer, *types.ContainerTaskInfo) error
	RunPreRun    func(container.Containerizer, *types.ContainerTaskInfo) error
	RunPostRun   func(container.Containerizer, *types.ContainerTaskInfo) error
	RunPreStop   func(container.Containerizer, *types.ContainerTaskInfo) error
	RunPostStop  func(container.Containerizer, *types.ContainerTaskInfo) error
}
