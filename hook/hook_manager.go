package hook

import (
	"fmt"

	"github.com/Devatoria/go-mesos-executor/logger"

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
}

// NewManager returns an empty HookManager (with no hooks)
func NewManager(hooks []string) *Manager {
	enabledHooks := make(map[string]struct{})
	for _, hook := range hooks {
		enabledHooks[hook] = struct{}{}
	}

	return &Manager{
		EnabledHooks:   enabledHooks,
		PreCreateHooks: []*Hook{},
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
		default:
			return fmt.Errorf("Unable to run a hook on %s", when)
		}
	}

	return nil
}

// RunPreCreateHooks runs all pre-create hooks of the given manager
func (m *Manager) RunPreCreateHooks() error {
	for _, hook := range m.PreCreateHooks {
		err := hook.Execute()
		if err != nil {
			logger.GetInstance().Production.Error(fmt.Sprintf("%s pre-create hook has failed", hook.Name), zap.Error(err))

			return err
		}
	}

	return nil
}
