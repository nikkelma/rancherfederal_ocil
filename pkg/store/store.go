package store

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"golang.org/x/sync/errgroup"
	"oras.land/oras-go/pkg/oras"
	"oras.land/oras-go/pkg/target"

	"github.com/rancherfederal/ocil/pkg/artifacts"
	"github.com/rancherfederal/ocil/pkg/content"
	"github.com/rancherfederal/ocil/pkg/layer"
)

type Layout struct {
	*content.OCI
	Root  string
	cache layer.Cache
}

type Options func(*Layout)

func WithCache(c layer.Cache) Options {
	return func(l *Layout) {
		l.cache = c
	}
}

func NewLayout(rootdir string, opts ...Options) (*Layout, error) {
	ociStore, err := content.NewOCI(rootdir)
	if err != nil {
		return nil, fmt.Errorf("new oci: %w", err)
	}

	if err := ociStore.LoadIndex(); err != nil {
		return nil, fmt.Errorf("load index: %w", err)
	}

	l := &Layout{
		Root: rootdir,
		OCI:  ociStore,
	}

	for _, opt := range opts {
		opt(l)
	}

	return l, nil
}

// AddOCI adds an artifacts.OCI to the store
//  The method to achieve this is to save artifact.OCI to a temporary directory in an OCI layout compatible form.  Once
//  saved, the entirety of the layout is copied to the store (which is just a registry).  This allows us to not only use
//  strict types to define generic content, but provides a processing pipeline suitable for extensibility.  In the
//  future we'll allow users to define their own content that must adhere either by artifact.OCI or simply an OCI layout.
func (l *Layout) AddOCI(ctx context.Context, oci artifacts.OCI, ref string) (ocispec.Descriptor, error) {
	if l.cache != nil {
		cached := layer.OCICache(oci, l.cache)
		oci = cached
	}

	// Write manifest blob
	m, err := oci.Manifest()
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("manifest: %w", err)
	}

	mdata, err := json.Marshal(m)
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("marshal manifest: %w", err)
	}
	if err := l.writeBlobData(mdata); err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("write blob data %w", err)
	}

	// Write config blob
	cdata, err := oci.RawConfig()
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("raw config: %w", err)
	}

	static.NewLayer(cdata, "")

	if err := l.writeBlobData(cdata); err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("write blob data: %w", err)
	}

	// write blob layers concurrently
	layers, err := oci.Layers()
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("layers: %w", err)
	}

	var g errgroup.Group
	for _, lyr := range layers {
		lyr := lyr
		g.Go(func() error {
			return l.writeLayer(lyr)
		})
	}
	if err := g.Wait(); err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("write layers: %w", err)
	}

	// Build index
	idx := ocispec.Descriptor{
		MediaType: string(m.MediaType),
		Digest:    digest.FromBytes(mdata),
		Size:      int64(len(mdata)),
		Annotations: map[string]string{
			ocispec.AnnotationRefName: ref,
		},
		URLs:     nil,
		Platform: nil,
	}
	err = l.OCI.AddIndex(idx)
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("add index: %w", err)
	}

	return idx, nil
}

// AddOCICollection .
func (l *Layout) AddOCICollection(ctx context.Context, collection artifacts.OCICollection) ([]ocispec.Descriptor, error) {
	cnts, err := collection.Contents()
	if err != nil {
		return nil, err
	}

	var descs []ocispec.Descriptor
	for ref, oci := range cnts {
		desc, err := l.AddOCI(ctx, oci, ref)
		if err != nil {
			return nil, err
		}
		descs = append(descs, desc)
	}
	return descs, nil
}

// Flush is a fancy name for delete-all-the-things, in this case it's as trivial as deleting oci-layout content
// 	This can be a highly destructive operation if the store's directory happens to be inline with other non-store contents
// 	To reduce the blast radius and likelihood of deleting things we don't own, Flush explicitly deletes oci-layout content only
func (l *Layout) Flush(ctx context.Context) error {
	blobs := filepath.Join(l.Root, "blobs")
	if err := os.RemoveAll(blobs); err != nil {
		return err
	}

	index := filepath.Join(l.Root, "index.json")
	if err := os.RemoveAll(index); err != nil {
		return err
	}

	layout := filepath.Join(l.Root, "oci-layout")
	if err := os.RemoveAll(layout); err != nil {
		return err
	}

	return nil
}

// Copy will copy a given reference to a given target.Target
// 		This is essentially a wrapper around oras.Copy, but locked to this content store
func (l *Layout) Copy(ctx context.Context, ref string, to target.Target, toRef string) (ocispec.Descriptor, error) {
	// if r, ok := to.(*ocontent.Registry); ok {
	// 	fmt.Println("ocil copy - found registry: %s", r.)
	// }

	// desc, err := oras.Copy(ctx, l.OCI, ref, to, toRef,
	// 	oras.WithAdditionalCachedMediaTypes(consts.DockerManifestSchema2))
	desc, err := oras.Copy(ctx, l.OCI, ref, to, toRef)

	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("oras copy: ref %s, toRef %s: %w", ref, toRef, err)
	}
	return desc, nil
}

// CopyAll performs bulk copy operations on the stores oci layout to a provided target.Target
func (l *Layout) CopyAll(ctx context.Context, to target.Target, toMapper func(string) (string, error)) ([]ocispec.Descriptor, error) {
	var descs []ocispec.Descriptor
	fmt.Println("THIS IS USING THE FORKED OCIL")
	err := l.OCI.Walk(func(reference string, desc ocispec.Descriptor) error {
		toRef := ""
		if toMapper != nil {
			tr, err := toMapper(reference)
			if err != nil {
				return fmt.Errorf("mapper: %w", err)
			}
			toRef = tr
		}

		desc, err := l.Copy(ctx, reference, to, toRef)
		if err != nil {
			return fmt.Errorf("layout copy: %w", err)
		}

		descs = append(descs, desc)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk: %w", err)
	}
	return descs, nil
}

// Identify is a helper function that will identify a human-readable content type given a descriptor
func (l *Layout) Identify(ctx context.Context, desc ocispec.Descriptor) string {
	rc, err := l.OCI.Fetch(ctx, desc)
	if err != nil {
		return ""
	}
	defer rc.Close()

	m := struct {
		Config struct {
			MediaType string `json:"mediaType"`
		} `json:"config"`
	}{}
	if err := json.NewDecoder(rc).Decode(&m); err != nil {
		return ""
	}

	return m.Config.MediaType
}

func (l *Layout) writeBlobData(data []byte) error {
	blob := static.NewLayer(data, "") // NOTE: MediaType isn't actually used in the writing
	return l.writeLayer(blob)
}

func (l *Layout) writeLayer(layer v1.Layer) error {
	d, err := layer.Digest()
	if err != nil {
		return err
	}

	r, err := layer.Compressed()
	if err != nil {
		return err
	}

	dir := filepath.Join(l.Root, "blobs", d.Algorithm)
	if err := os.MkdirAll(dir, os.ModePerm); err != nil && !os.IsExist(err) {
		return err
	}

	blobPath := filepath.Join(dir, d.Hex)
	// Skip entirely if something exists, assume layer is present already
	if _, err := os.Stat(blobPath); err == nil {
		return nil
	}

	w, err := os.Create(blobPath)
	if err != nil {
		return err
	}
	defer w.Close()

	_, err = io.Copy(w, r)
	return err
}
