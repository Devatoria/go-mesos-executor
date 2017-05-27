package hook

import (
	"github.com/Devatoria/go-mesos-executor/types"
)

// Hook represents an executable hook (we don't care if it's a pre-create, post-stop or whatever)
type Hook struct {
	Execute  func(*types.ContainerTaskInfo) error
	Name     string
	Priority int64
}
