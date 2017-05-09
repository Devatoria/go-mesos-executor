package container

// Containerizer represents a containerizing technology such as docker
type Containerizer interface {
	ContainerRun(name, image string) (string, error)
	ContainerStop(id string) error
}
