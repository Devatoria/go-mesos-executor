package hook

import (
	"fmt"

	"github.com/Devatoria/go-mesos-executor/container"

	"github.com/mesos/mesos-go/api/v1/lib"
)


// PrivilegeHook avoid a container to start with privilege mode
var PrivilegeHook = Hook{
	Name:     "privilege",
	Priority: 0,
	RunPreCreate: func(c container.Containerizer, info *mesos.TaskInfo) error {
		if *info.Container.Docker.Privileged == true {
			return fmt.Errorf(
				"privilege mode is disable",
			)
		}
		return nil
	},
}
