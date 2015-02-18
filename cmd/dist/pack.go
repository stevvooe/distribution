package main

import (
	"os"
	"runtime"

	"github.com/codegangsta/cli"
	ctxu "github.com/docker/distribution/context"
	"github.com/docker/distribution/digest"
	"github.com/docker/distribution/manifest"
	"github.com/docker/distribution/registry/api/v2"
	"github.com/docker/distribution/registry/storage"
	"github.com/docker/distribution/registry/storage/driver/filesystem"
	"github.com/docker/libtrust"
	"golang.org/x/net/context"
)

var (
	commandPack = cli.Command{
		Name:   "pack",
		Usage:  "pack a manifest",
		Action: pack,
		Flags: []cli.Flag{
			cli.BoolFlag{
				Name:  "p,print",
				Usage: "Dump the manifest to stdout rather than its hash",
			},
		},
	}
)

func pack(c *cli.Context) {
	ctx := context.Background()
	local := storage.NewRegistryWithDriver(filesystem.New("/tmp/local-registry"))

	name, tag, revision := splitNameTag(c.Args().Get(0))
	if revision != "" {
		ctxu.GetLogger(ctx).Fatalf("revision not required: %q", c.Args().Get(0))
	}

	if tag == "" {
		ctxu.GetLogger(ctx).Fatalf("tag required: %q", c.Args().Get(0))
	}

	layers := c.Args()[1:]

	if err := v2.ValidateRespositoryName(name); err != nil {
		ctxu.GetLogger(ctx).Fatalf("provided repository name not valid: %v", err)
	}

	repo, err := local.Repository(ctx, name)
	if err != nil {
		ctxu.GetLogger(ctx).Fatalf("error getting repository: %v", err)
	}

	pk, err := libtrust.GenerateECP256PrivateKey()
	if err != nil {
		ctxu.GetLogger(ctx).Fatalf("error getting repository: %v", err)
	}

	var m manifest.Manifest

	m.SchemaVersion = 1
	m.Name = name
	m.Tag = tag
	m.Architecture = runtime.GOARCH

	for _, layerDigestArg := range layers {
		dgst, err := digest.ParseDigest(layerDigestArg)
		if err != nil {
			ctxu.GetLogger(ctx).Fatalf("error parsing digest: %v", err)
		}

		m.FSLayers = append(m.FSLayers, manifest.FSLayer{BlobSum: dgst})
	}

	sm, err := manifest.Sign(&m, pk)
	if err != nil {
		ctxu.GetLogger(ctx).Fatalf("error signing manifest: %v", err)
	}

	if c.Bool("print") {
		os.Stdout.Write(sm.Raw)
	}

	if err := repo.Manifests().Put(sm.Tag, sm); err != nil {
		ctxu.GetLogger(ctx).Fatalf("error storing manifest: %v", err)
	}
}
