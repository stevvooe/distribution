package manifest

import (
	"fmt"

	"github.com/docker/distribution"
	"github.com/docker/distribution/digest"
	"github.com/docker/libtrust"
)

type ManifestBuilder struct {
	Manifest
	pk libtrust.PrivateKey
}

// NewManifestBuilder is used to build new manifests for the current schema
// version.
func NewManifestBuilder(pk libtrust.PrivateKey, name, tag, architecture string) distribution.ManifestBuilder {
	return &ManifestBuilder{
		Manifest: Manifest{
			Versioned: Versioned{
				SchemaVersion: 1,
			},
			Name:         name,
			Tag:          tag,
			Architecture: architecture,
		},
		pk: pk,
	}
}

func (mb *ManifestBuilder) Build() (distribution.Manifest, error) {
	m := mb.Manifest
	m.FSLayers = make([]FSLayer, len(mb.Manifest.FSLayers))
	m.History = make([]History, len(mb.Manifest.History))
	copy(m.FSLayers, mb.Manifest.FSLayers)
	copy(m.History, mb.Manifest.History)

	return Sign(&m, mb.pk)
}

func (mb *ManifestBuilder) AddDependency(dependency distribution.Describable) error {
	switch v := dependency.(type) {
	// NOTE(stevvooe): This manifest version only supports FSLayer/History
	// style dependencies.
	case *Dependency:

		// Entries need to be prepended
		mb.Manifest.FSLayers = append([]FSLayer{FSLayer{BlobSum: v.Digest}}, mb.Manifest.FSLayers...)
		mb.Manifest.History = append([]History{v.History}, mb.Manifest.History...)
		return nil
	}

	return fmt.Errorf("manifest builder: dependency not supported: %v", dependency)
}

// Dependency describes a manifest v2, schema version 1 dependency.
// Effectively, we have an FSLayer associated with a history entry.
type Dependency struct {
	Digest  digest.Digest
	Length  int64 // if we know it, set it for the descriptor.
	History History
}

func (d Dependency) Descriptor() distribution.Descriptor {
	return distribution.Descriptor{
		MediaType: "application/vnd.docker.container.image.rootfs.diff+x-gtar",
		Digest:    d.Digest,
		Length:    d.Length,
	}
}
