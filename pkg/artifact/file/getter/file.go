package getter

import (
	"context"
	"io"
	"net/url"
	"os"
	"path/filepath"

	"github.com/rancherfederal/oci-artifacts/pkg/artifact"
	"github.com/rancherfederal/oci-artifacts/pkg/consts"
)

type File struct{}

func NewFile() *File {
	return &File{}
}

func (f File) Name(u *url.URL) string {
	return filepath.Base(f.path(u))
}

func (f File) Open(ctx context.Context, u *url.URL) (io.ReadCloser, error) {
	return os.Open(f.path(u))
}

func (f File) Detect(u *url.URL) bool {
	if len(f.path(u)) == 0 {
		return false
	}

	fi, err := os.Stat(f.path(u))
	if err != nil {
		return false
	}
	return !fi.IsDir()
}

func (f File) path(u *url.URL) string {
	return filepath.Join(u.Host, u.Path)
}

func (f File) Config(u *url.URL) artifact.Config {
	c := &fileConfig{
		config{Reference: u.String()},
	}
	return artifact.ToConfig(c, artifact.WithConfigMediaType(consts.FileLocalConfigMediaType))
}

type fileConfig struct {
	config `json:",inline,omitempty"`
}
