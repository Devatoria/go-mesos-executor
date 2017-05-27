package hook

// Hook represents an executable hook (we don't care if it's a pre-create, post-stop or whatever)
type Hook struct {
	Execute  func() error
	Name     string
	Priority int64
}
