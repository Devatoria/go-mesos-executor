package hook

import (
	"fmt"
	"sort"

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
	EnabledHooks map[string]struct{}
	Hooks        []*Hook
}

// sorter is a sort interface implementation in order to sort hooks
type sorter struct {
	hooks []*Hook
	by    func(h1, h2 *Hook) bool
}

type when string

const (
	preCreate = "pre-create"
	preRun    = "pre-run"
	postRun   = "post-run"
	preStop   = "pre-stop"
	postStop  = "post-stop"
)

// Len is part of the sort interface
func (s *sorter) Len() int {
	return len(s.hooks)
}

// Less is part of the sort interface
func (s *sorter) Less(i, j int) bool {
	return s.by(s.hooks[i], s.hooks[j])
}

// Swap is part of the sort interface
func (s *sorter) Swap(i, j int) {
	s.hooks[i], s.hooks[j] = s.hooks[j], s.hooks[i]
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

// sort sorts all slices using the given by function
func (m *Manager) sort(by func(h1, h2 *Hook) bool) {
	hookSorter := &sorter{m.Hooks, by}
	sort.Sort(hookSorter)
}

// sortByPriority sorts all slices by descending priority
func (m *Manager) sortByPriority() {
	m.sort(func(h1, h2 *Hook) bool {
		return !(h1.Priority < h2.Priority)
	})
}

// RegisterHooks registers a list of hooks on the given "when" (pre-create, ...)
// It throws an error in case of the given "when" is incorrect
func (m *Manager) RegisterHooks(hooks ...*Hook) error {
	for _, hook := range hooks {
		// Pass on disabled hooks
		if _, ok := m.EnabledHooks[hook.Name]; !ok {
			logger.GetInstance().Debug(fmt.Sprintf("Disabling %s hook", hook.Name))
			continue
		}

		m.Hooks = append(m.Hooks, hook)
	}

	// Re-sort slices by priority
	m.sortByPriority()

	return nil
}

// RunPreCreateHooks runs all pre-create hooks of the given manager
func (m *Manager) RunPreCreateHooks(c container.Containerizer, info *types.ContainerTaskInfo) error {
	return m.runHooks(preCreate, c, info, true)
}

// RunPreRunHooks runs all pre-create hooks of the given manager
func (m *Manager) RunPreRunHooks(c container.Containerizer, info *types.ContainerTaskInfo) error {
	return m.runHooks(preRun, c, info, true)
}

// RunPostRunHooks runs all pre-create hooks of the given manager
func (m *Manager) RunPostRunHooks(c container.Containerizer, info *types.ContainerTaskInfo) error {
	return m.runHooks(postRun, c, info, true)
}

// RunPreStopHooks runs all pre-create hooks of the given manager
func (m *Manager) RunPreStopHooks(c container.Containerizer, info *types.ContainerTaskInfo) error {
	return m.runHooks(preStop, c, info, false)
}

// RunPostStopHooks runs all pre-create hooks of the given manager
func (m *Manager) RunPostStopHooks(c container.Containerizer, info *types.ContainerTaskInfo) error {
	return m.runHooks(postStop, c, info, false)
}

func (m *Manager) runHooks(w when, c container.Containerizer, info *types.ContainerTaskInfo, exitOnError bool) error {
	for _, hook := range m.Hooks {
		logger.GetInstance().Info("Running a hook",
			zap.String("hook", hook.Name),
			zap.String("when", string(w)),
		)

		var err error
		switch w {
		case preCreate:
			if hook.RunPreCreate == nil {
				continue
			}

			err = hook.RunPreCreate(c, info)
		case preRun:
			if hook.RunPreRun == nil {
				continue
			}

			err = hook.RunPreRun(c, info)
		case postRun:
			if hook.RunPostRun == nil {
				continue
			}

			err = hook.RunPostRun(c, info)
		case preStop:
			if hook.RunPreStop == nil {
				continue
			}

			err = hook.RunPreStop(c, info)
		case postStop:
			if hook.RunPostStop == nil {
				continue
			}

			err = hook.RunPostStop(c, info)
		default:
			return fmt.Errorf("")
		}

		if err != nil {
			logger.GetInstance().Error(fmt.Sprintf("%s %s hook has failed", w, hook.Name), zap.Error(err))

			if exitOnError {
				return err
			}
		}
	}

	return nil
}
