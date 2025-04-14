package trace

type UserTraceeOptions struct {
	exePath string
	// TODO(maxgio92): allow to specify functions to trace.
}

type UserTraceeOption func(*UserTracee)

func WithExePath(path string) UserTraceeOption {
	return func(o *UserTracee) {
		o.exePath = path
	}
}
