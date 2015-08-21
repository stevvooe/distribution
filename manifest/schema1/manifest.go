package schema1

import (
	"encoding/json"
	"fmt"

	"github.com/docker/distribution"
	"github.com/docker/distribution/digest"
	"github.com/docker/distribution/manifest"
	"github.com/docker/libtrust"
)

// TODO(stevvooe): When we rev the manifest format, the contents of this
// package should be moved to manifest/v1.

const (
	// MediaTypeManifest specifies the mediaType for the current version. Note
	// that for schema version 1, the the media is optionally
	// "application/json".
	MediaTypeManifest       = "application/vnd.docker.distribution.manifest.v1+json"
	MediaTypeSignedManifest = "application/vnd.docker.distribution.manifest.v1+prettyjws"
)

var (
	// SchemaVersion provides a pre-initialized version structure for this
	// packages version of the manifest.
	SchemaVersion = manifest.Versioned{
		SchemaVersion: 1,
	}
)

// FSLayer is a container struct for BlobSums defined in an image manifest
type FSLayer struct {
	// BlobSum is the tarsum of the referenced filesystem image layer
	BlobSum digest.Digest `json:"blobSum"`
}

// History stores unstructured v1 compatibility information
type History struct {
	// V1Compatibility is the raw v1 compatibility information
	V1Compatibility string `json:"v1Compatibility"`
}

// Manifest provides the base accessible fields for working with V2 image
// format in the registry.
type Manifest struct {
	manifest.Versioned

	// Name is the name of the image's repository
	Name string `json:"name"`

	// Tag is the tag of the image specified by this manifest
	Tag string `json:"tag"`

	// Architecture is the host architecture on which this image is intended to
	// run
	Architecture string `json:"architecture"`

	// FSLayers is a list of filesystem layer blobSums contained in this image
	FSLayers []FSLayer `json:"fsLayers"`

	// History is a list of unstructured historical data for v1 compatibility
	History []History `json:"history"`
}

func (m Manifest) Dependencies() []distribution.Descriptor {
	// Bah, this format is junk:
	//	1. We don't know the size, so it won't be specified in the descriptor.
	//       Conversions to new manifest types will have to include layer
	//       size.
	//  2. FSLayers is in the wrong order. Must iterate over it backwards

	dependencies := make([]distribution.Descriptor, len(m.FSLayers))
	for i := len(m.FSLayers) - 1; i >= 0; i-- {
		fsLayer := m.FSLayers[i]
		dependencies[len(m.FSLayers)-i] = distribution.Descriptor{
			MediaType: "application/vnd.docker.container.image.rootfs.diff+x-gtar",
			Digest:    fsLayer.BlobSum,
		}
	}

	return dependencies
}

func (m *Manifest) WithSignatures(signatures ...[]byte) (distribution.Manifest, error) {
	var sm SignedManifest
	sm.Manifest = manifest
	return sm, sm.AddSignatures(signatures...)
}

// SignedManifest provides an envelope for a signed image manifest, including
// the format sensitive raw bytes. It contains fields to
type SignedManifest struct {
	Manifest

	// Raw is the byte representation of the ImageManifest, used for signature
	// verification. The value of Raw must be used directly during
	// serialization, or the signature check will fail. The manifest byte
	// representation cannot change or it will have to be re-signed.
	Raw []byte `json:"-"`
}

// UnmarshalJSON populates a new ImageManifest struct from JSON data.
func (sm *SignedManifest) UnmarshalJSON(b []byte) error {
	var manifest Manifest
	if err := json.Unmarshal(b, &manifest); err != nil {
		return err
	}

	sm.Manifest = manifest
	sm.Raw = make([]byte, len(b), len(b))
	copy(sm.Raw, b)

	return nil
}

// MarshalJSON returns the contents of raw. If Raw is nil, marshals the inner
// contents. Applications requiring a marshaled signed manifest should simply
// use Raw directly, since the the content produced by json.Marshal will be
// compacted and will fail signature checks.
func (sm *SignedManifest) MarshalJSON() ([]byte, error) {
	if len(sm.Raw) > 0 {
		return sm.Raw, nil
	}

	// If the raw data is not available, just dump the inner content.
	return json.Marshal(&sm.Manifest)
}

// Payload returns the raw, signed content of the signed manifest. The
// contents can be used to calculate the content identifier.
func (sm *SignedManifest) Payload() ([]byte, error) {
	jsig, err := libtrust.ParsePrettySignature(sm.Raw, "signatures")
	if err != nil {
		return nil, err
	}

	// Resolve the payload in the manifest.
	return jsig.Payload()
}

// Signatures returns the signatures as provided by
// (*libtrust.JSONSignature).Signatures. The byte slices are opaque jws
// signatures.
func (sm *SignedManifest) Signatures() ([][]byte, error) {
	jsig, err := libtrust.ParsePrettySignature(sm.Raw, "signatures")
	if err != nil {
		return nil, err
	}

	// Resolve the payload in the manifest.
	return jsig.Signatures()
}

// AddSignatures adds the provided signatures to the signed manifest, updated
// the contents of Raw accordingly. This requires a serialization round trip,
// so don't call this very often.
func (sm *SignedManifest) AddSignatures(signatures ...[]byte) error {
	payload, err := sm.Payload()
	if err != nil {
		return err
	}

	existing, err := sm.Signatures()
	if err != nil {
		return err
	}

	signatures = append(existing, signatures...)

	jsig, err := libtrust.NewJSONSignature(payload, signatures...)
	if err != nil {
		return nil, err
	}

	// Extract the pretty JWS
	raw, err := jsig.PrettySignature("signatures")
	if err != nil {
		return nil, err
	}

	sm.Raw = raw // signatures are effectively stored in Raw

	// ensure content matches up with added signatures
	if _, err = Verify(sm); err != nil {
		return fmt.Errorf("error adding signatures: %v", err)
	}

	return nil
}

func init() {
	distribution.RegisterManifestSchema("application/json", SignedManifest{}) // effectively, this is the default type.
	distribution.RegisterManifestSchema(MediaTypeManifest, Manifest{})
	distribution.RegisterManifestSchema(MediaTypeSignedManifest, SignedManifest{})
}
