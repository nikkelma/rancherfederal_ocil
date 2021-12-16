package getter_test

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/rancherfederal/oci-artifacts/pkg/artifact/file/getter"
)

func TestClient_Detect(t *testing.T) {
	teardown := setup(t)
	defer teardown()

	c := getter.NewClient(getter.ClientOptions{})

	type args struct {
		source string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "should identify a file",
			args: args{
				source: fileWithExt,
			},
			want: "file",
		},
		{
			name: "should identify a directory",
			args: args{
				source: rootDir,
			},
			want: "directory",
		},
		{
			name: "should identify a http",
			args: args{
				source: "http://my.cool.website",
			},
			want: "http",
		},
		{
			name: "should identify a http",
			args: args{
				source: "https://my.cool.website",
			},
			want: "http",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := identify(c, tt.args.source); got != tt.want {
				t.Errorf("identify() = %v, want %v", got, tt.want)
			}
		})
	}
}

func identify(c *getter.Client, source string) string {
	u, _ := url.Parse(source)
	for t, g := range c.Getters {
		if g.Detect(u) {
			return t
		}
	}
	return ""
}

func TestClient_Name(t *testing.T) {
	teardown := setup(t)
	defer teardown()

	c := getter.NewClient(getter.ClientOptions{})

	type args struct {
		source string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "should correctly name a file with an extension",
			args: args{
				source: fileWithExt,
			},
			want: "file.yaml",
		},
		{
			name: "should correctly name a directory",
			args: args{
				source: rootDir,
			},
			want: rootDir,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := c.Name(tt.args.source); got != tt.want {
				t.Errorf("Name() = %v, want %v", got, tt.want)
			}
		})
	}
}

var (
	rootDir     = "gettertests"
	fileWithExt = filepath.Join(rootDir, "file.yaml")
)

func setup(t *testing.T) func() {
	if err := os.MkdirAll(rootDir, os.ModePerm); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(fileWithExt, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	return func() {
		os.RemoveAll(rootDir)
	}
}
