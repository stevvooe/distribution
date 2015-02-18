package main

import (
	"net/http"

	"github.com/codegangsta/cli"
	"github.com/docker/distribution"
	ctxu "github.com/docker/distribution/context"
	"github.com/docker/distribution/registry/client"
	"github.com/docker/distribution/registry/storage"
	"github.com/docker/distribution/registry/storage/driver/filesystem"
	"golang.org/x/net/context"
)

var (
	commandPull = cli.Command{
		Name:   "pull",
		Usage:  "Pull and verify an image from a registry",
		Action: imagePull,
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:  "r,registry",
				Value: "http://localhost:5000/",
				Usage: "Registry to use (e.g.: localhost:5000)",
			},
		},
	}
)

func imagePull(c *cli.Context) {
	ctx := context.Background()

	local := storage.NewRegistryWithDriver(filesystem.New("/tmp/local-registry"))
	remote, err := client.NewRegistryClient(http.DefaultClient, c.String("registry"))
	if err != nil {
		ctxu.GetLogger(ctx).Fatalln(err)
	}

	if err := distribution.Pull(ctx, local, remote, "library/ubuntu", "latest"); err != nil {
		ctxu.GetLogger(ctx).Fatalln(err)
	}
}
