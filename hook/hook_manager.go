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
	EnabledHooks   map[string]struct{}
	PreCreateHooks []*Hook
	PreRunHooks    []*Hook
	PostRunHooks   []*Hook
	PreStopHooks   []*Hook
	PostStopHooks  []*Hook
}

// sorter is a sort interface implementation in order to sort hooks
type sorter struct {
	hooks []*Hook
	by    func(h1, h2 *Hook) bool
}

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
	preCreateSorter := &sorter{m.PreCreateHooks, by}
	preRunSorter := &sorter{m.PreRunHooks, by}
	postRunSorter := &sorter{m.PostRunHooks, by}
	preStopSorter := &sorter{m.PreStopHooks, by}
	postStopSorter := &sorter{m.PostStopHooks, by}

	sort.Sort(preCreateSorter)
	sort.Sort(preRunSorter)
	sort.Sort(postRunSorter)
	sort.Sort(preStopSorter)
	sort.Sort(postStopSorter)
}

// sortByPriority sorts all slices by descending priority
func (m *Manager) sortByPriority() {
	m.sort(func(h1, h2 *Hook) bool {
		return !(h1.Priority < h2.Priority)
	})
}

// RegisterHooks registers a list of hooks on the given "when" (pre-create, ...)
// It throws an error in case of the given "when" is incorrect
func (m *Manager) RegisterHooks(when string, hooks ...*Hook) error {
	for _, hook := range hooks {
		// Pass on disabled hooks
		if _, ok := m.EnabledHooks[hook.Name]; !ok {
			logger.GetInstance().Debug(fmt.Sprintf("Disabling %s %s hook", hook.Name, when))
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

	// Re-sort slices by priority
	m.sortByPriority()

	return nil
}

// RunPreCreateHooks runs all pre-create hooks of the given manager
func (m *Manager) RunPreCreateHooks(c container.Containerizer, info *types.ContainerTaskInfo) error {
	for _, hook := range m.PreCreateHooks {
		err := hook.Execute(c, info)
		if err != nil {
			logger.GetInstance().Error(fmt.Sprintf("%s pre-create hook has failed", hook.Name), zap.Error(err))

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
			logger.GetInstance().Error(fmt.Sprintf("%s pre-run hook has failed", hook.Name), zap.Error(err))

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
			logger.GetInstance().Error(fmt.Sprintf("%s post-run hook has failed", hook.Name), zap.Error(err))

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
			logger.GetInstance().Error(fmt.Sprintf("%s pre-stop hook has failed", hook.Name), zap.Error(err))

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
			logger.GetInstance().Error(fmt.Sprintf("%s post-stop hook has failed", hook.Name), zap.Error(err))

			return err
		}
	}

	return nil
}
