package filesystem

import (
	"os"
)

type FS interface {
	ReadFile(name string) ([]byte, error)
}

var AllowAll FS = &allowAll{}

type allowAll struct{}

func (r *allowAll) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}
