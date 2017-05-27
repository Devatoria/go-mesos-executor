package hook

import (
	"fmt"

	"github.com/Devatoria/go-mesos-executor/container"
	"github.com/Devatoria/go-mesos-executor/logger"
	"github.com/Devatoria/go-mesos-executor/types"

	"go.uber.org/zap"
)

// Manager is a hook manager with different kinds of hooks:
// - pre-create
// - pre-run
// - post-run
// - pre-stop
// - post-stop
// It also contains a list of enabled hooks names
type Manager struct {
	EnabledHooks   map[string]struct{}
	PreCreateHooks []*Hook
	PreRunHooks    []*Hook
	PostRunHooks   []*Hook
	PreStopHooks   []*Hook
	PostStopHooks  []*Hook
}

// NewManager returns an empty HookManager (with no hooks)
func NewManager(hooks []string) *Manager {
	enabledHooks := make(map[string]struct{})
	for _, hook := range hooks {
		enabledHooks[hook] = struct{}{}
	}

	return &Manager{
		EnabledHooks: enabledHooks,
	}
}

// RegisterHooks registers a list of hooks on the given "when" (pre-create, ...)
// It throws an error in case of the given "when" is incorrect
func (m *Manager) RegisterHooks(when string, hooks ...*Hook) error {
	for _, hook := range hooks {
		// Pass on disabled hooks
		if _, ok := m.EnabledHooks[hook.Name]; !ok {
			logger.GetInstance().Development.Debug(fmt.Sprintf("Disabling %s %s hook", hook.Name, when))
			continue
		}

		switch when {
		case "pre-create":
			m.PreCreateHooks = append(m.PreCreateHooks, hook)
		case "pre-run":
			m.PreRunHooks = append(m.PreRunHooks, hook)
		case "post-run":
			m.PostRunHooks = append(m.PostRunHooks, hook)
		case "pre-stop":
			m.PreStopHooks = append(m.PreStopHooks, hook)
		case "post-stop":
			m.PostStopHooks = append(m.PostStopHooks, hook)
		default:
			return fmt.Errorf("Unable to run a hook on %s", when)
		}
	}

	return nil
}

// RunPreCreateHooks runs all pre-create hooks of the given manager
func (m *Manager) RunPreCreateHooks(c container.Containerizer, info *types.ContainerTaskInfo) error {
	for _, hook := range m.PreCreateHooks {
		err := hook.Execute(c, info)
		if err != nil {
			logger.GetInstance().Production.Error(fmt.Sprintf("%s pre-create hook has failed", hook.Name), zap.Error(err))

			return err
		}
	}

	return nil
}

// RunPreRunHooks runs all pre-create hooks of the given manager
func (m *Manager) RunPreRunHooks(c container.Containerizer, info *types.ContainerTaskInfo) error {
	for _, hook := range m.PreRunHooks {
		err := hook.Execute(c, info)
		if err != nil {
			logger.GetInstance().Production.Error(fmt.Sprintf("%s pre-run hook has failed", hook.Name), zap.Error(err))

			return err
		}
	}

	return nil
}

// RunPostRunHooks runs all pre-create hooks of the given manager
func (m *Manager) RunPostRunHooks(c container.Containerizer, info *types.ContainerTaskInfo) error {
	for _, hook := range m.PostRunHooks {
		err := hook.Execute(c, info)
		if err != nil {
			logger.GetInstance().Production.Error(fmt.Sprintf("%s post-run hook has failed", hook.Name), zap.Error(err))

			return err
		}
	}

	return nil
}

// RunPreStopHooks runs all pre-create hooks of the given manager
func (m *Manager) RunPreStopHooks(c container.Containerizer, info *types.ContainerTaskInfo) error {
	for _, hook := range m.PreStopHooks {
		err := hook.Execute(c, info)
		if err != nil {
			logger.GetInstance().Production.Error(fmt.Sprintf("%s pre-stop hook has failed", hook.Name), zap.Error(err))

			return err
		}
	}

	return nil
}

// RunPostStopHooks runs all pre-create hooks of the given manager
func (m *Manager) RunPostStopHooks(c container.Containerizer, info *types.ContainerTaskInfo) error {
	for _, hook := range m.PostStopHooks {
		err := hook.Execute(c, info)
		if err != nil {
			logger.GetInstance().Production.Error(fmt.Sprintf("%s post-stop hook has failed", hook.Name), zap.Error(err))

			return err
		}
	}

	return nil
}
