package distribution

import (
	"github.com/docker/distribution"
	"github.com/docker/distribution/context"
	"github.com/docker/distribution/digest"
	"github.com/docker/distribution/manifest"
)

// Manifest specifies a registry object with a list of dependencies.
type Manifest interface {
	Target() Descriptor

	// Dependencies returns a list of object on which this manifest depends.
	// The dependencies are strictly ordered from base to head. Typically,
	// these are layers but their interpretation is application specific.
	Dependencies() []Descriptor

	// Payload provides the serialized format of the manifest, in addition to
	// the mediatype. It may not be entirely necessary but we at least need to
	// detect the mediatype.
	Payload() (mediatype string, payload []byte, err error) // TODO(stevvooe): Think about this more.

	// TODO(stevvooe): Possibly expose labels here.
}

type Signed interface {
	Signatures() ([][]byte, error)
}

// SignableManifest allows one to add signatures to the underlying serialized
// manifest. This is a terrible name.
type SignableManifest interface {
	WithSignatures(signatures ...[]byte) (distribution.Manifest, error)
}

// ManifestBuilder creates a manifest allowing one to include dependencies.
// Instances can be obtained from a version-specific manifest package, likely
// from a type that provides a version specific interface.
type ManifestBuilder interface {
	// Build creates the manifest fromt his builder.
	Build() (Manifest, error)

	// Dependencies returns a list of objects which have been added to this
	// builder. The dependencies are returned in the order they were added,
	// which should be from base to head.
	Dependencies() []Descriptor

	// AddDependency includes the dependency in the manifest after any
	// existing dependencies. If the add fails, such as when adding an
	// unsupported dependency, an error may be returned.
	AddDependency(dependency Descriptor) error
}

// ManifestService describes operations on image manifests.
type ManifestService interface {
	// Exists returns true if the manifest exists.
	Exists(ctx context.Context, dgst digest.Digest) (bool, error)

	// Get retrieves the named manifest, if it exists.
	Get(ctx context.Context, dgst digest.Digest) (Manifest, error)

	// Put creates or updates the named manifest.
	Put(ctx context.Context, manifest Manifest) (digest.Digest, error)

	// Delete removes the named manifest, if it exists.
	Delete(ctx context.Context, dgst digest.Digest) error

	// TODO(stevvooe): Provide All() method that lets one scroll through all
	// of the manifest entries.

	// TODO(stevvooe): The methods after this message should be moved to a
	// discrete TagService, per active proposals.

	// Tags lists the tags under the named repository.
	Tags() ([]string, error)

	// ExistsByTag returns true if the manifest exists.
	ExistsByTag(tag string) (bool, error)

	// GetByTag retrieves the named manifest, if it exists.
	GetByTag(tag string, options ...ManifestServiceOption) (*manifest.SignedManifest, error)

	// TODO(stevvooe): There are several changes that need to be done to this
	// interface:
	//
	//      1. Allow explicit tagging with Tag(digest digest.Digest, tag string)
	//      2. Support reading tags with a re-entrant reader to avoid large
	//       allocations in the registry.
	//      3. Long-term: Provide All() method that lets one scroll through all of
	//       the manifest entries.
	//      4. Long-term: break out concept of signing from manifests. This is
	//       really a part of the distribution sprint.
	//      5. Long-term: Manifest should be an interface. This code shouldn't
	//       really be concerned with the storage format.
}

// TODO(stevvooe): This likely belongs in the manifest package (not schema1).

// SignedManifestService is a demonstration of assembling higher-level
// services compositionally, rather than hiding the interface wiring behind
// the scenes.
type SignedManifestService struct {
	ManifestService
	signatures SignatureService
}

func NewSignedManifestService(manifests ManifestService, signatures SignatureService) ManifestService {
	return &SignedManifestService{
		ManifestService: ManifestService,
		signatures:      signatures,
	}
}

func (sms *SignedManifestService) Get(ctx context.Context, dgst digest.Digest) (Manifest, error) {
	m, err := sms.ManifestService.Get(ctx, dgst)
	if err != nil {
		return nil, err
	}

	if signable, ok := m.(SignableManifest); !ok {
		return m, nil // not-signable, move along.
	}

	signatures, err := sms.signatures.Get(dgst)
	if err != nil {
		return nil, err
	}

	sm, err := signable.WithSignatures(signatures...)
	if err != nil {
		return nil, err
	}

	return sm, nil
}

func (sms *SignedManifestService) Put(ctx context.Context, manifest Manifest) (digest.Digest, error) {
	dgst, err := sms.ManifestService.Put(ctx, manifest)
	if err != nil {
		return "", err
	}

	if sm, ok := manifest.(Signed); !ok {
		return dgst, nil // not signed, move along
	}

	signatures, err := sm.Signatures()
	if err != nil {
		return "", err
	}

	return dgst, sms.signatures.Put(dgst, signatures...)
}

// ManifestServiceImpl is a thought experiment for making a manifest service a
// completely concrete type based only on a BlobService. The missing component
// to making this happen is getting all of the tag methods off of manifest
// service. The magic here actually lies in how we create the BlobService. For
// the registry implementation, we generally have to control per repository
// access to this blob service. This principle is simply applied at this
// level: the BlobService would be a linkedBlobStore instance, in the storage
// package parlance. For a remote client, the concept is the same: this
// BlobService instance would be an http client that hits the /manifests/
// endpoint, in addition to the standard /blobs/ endpoint, in case the
// manifest is stored directly in the blob store.
type ManifestServiceImpl struct {
	bs BlobService
}

func NewManifestService(bs BlobService) (ManifestService, error) {
	return &ManifestService{
		bs: bs,
	}
}

func (ms *ManifestServiceImpl) Exists(ctx context.Context, dgst digest.Digest) (bool, error) {
	return false, nil
}

func (ms *ManifestServiceImpl) Put(ctx context.Context, manifest Manifest) (digest.Digest, error) {
	mediaType, p, err := manifest.Payload()
	if err != nil {
		return "", err
	}

	return ms.bs.Put(ctx, mediaType, p)
}

func (ms *ManifestServiceImpl) Get(ctx context.Context, dgst digest.Digest) (Manifest, error) {
	desc, err := ms.bs.Stat(ctx, dgst)
	if err != nil {
		return nil, err
	}

	p, err := ms.bs.Get(ctx, dgst)
	if err != nil {
		return nil, err
	}

	return UnmarshalManifest(desc.MediaType, p)
}

// UnmarshalManifest attempts to resolve the manifest format based on the
// mediatype. If the mediatype is unknown or doesn't provide a strong enough
// hint, several formats will be tried.
func UnmarshalManifest(mediatype string, p []byte) (Manifest, error) {

}

func RegisterManifestSchema(mediatype string, v Manifest) {

}
