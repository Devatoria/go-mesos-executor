package hook

import (
	"github.com/Devatoria/go-mesos-executor/container"

	"github.com/mesos/mesos-go/api/v1/lib"
)

// Hook represents an executable hook (we don't care if it's a pre-create, post-stop or whatever)
type Hook struct {
	Name         string
	Priority     int64
	RunPreCreate func(container.Containerizer, *mesos.TaskInfo) error
	RunPreRun    func(container.Containerizer, *mesos.TaskInfo, string) error
	RunPostRun   func(container.Containerizer, *mesos.TaskInfo, string) error
	RunPreStop   func(container.Containerizer, *mesos.TaskInfo, string) error
	RunPostStop  func(container.Containerizer, *mesos.TaskInfo, string) error
}
