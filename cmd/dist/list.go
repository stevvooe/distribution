package main

import (
	"fmt"
	"os"
	"path"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/codegangsta/cli"
	ctxu "github.com/docker/distribution/context"
	"github.com/docker/distribution/digest"
	"github.com/docker/distribution/registry/storage"
	"github.com/docker/distribution/registry/storage/driver"
	"github.com/docker/distribution/registry/storage/driver/filesystem"
	"github.com/dustin/go-humanize"
	"golang.org/x/net/context"
)

var (
	commandList = cli.Command{
		Name:   "images",
		Usage:  "List available images",
		Action: imageList,
	}
)

func imageList(c *cli.Context) {
	driver := filesystem.New("/tmp/local-registry")
	local := storage.NewRegistryWithDriver(driver)
	ctx := context.Background()

	wr := tabwriter.NewWriter(os.Stdout, 0, 8, 0, '\t', 0)
	fmt.Fprintf(wr, "REPOSITORY\tTAG\tIMAGE ID\tCREATED\tVIRTUAL SIZE\n")

	prefix := "/docker/registry/v2/repositories"

	// TODO(stevvooe): Need way to list repositories
	walk(driver, prefix, func(p string) error {
		if strings.HasPrefix(path.Base(p), "_") {
			return fmt.Errorf("stop")
		}

		name := strings.TrimPrefix(p, prefix+"/")
		if strings.Count(name, "/") < 1 {
			return nil
		}

		repo, err := local.Repository(ctx, name)
		if err != nil {
			ctxu.GetLogger(ctx).Fatalf("error getting repository: %v", err)
		}

		tags, err := repo.Manifests().Tags()
		if err != nil {
			ctxu.GetLogger(ctx).Fatalf("error reading tags: %v", err)
		}

		for _, tag := range tags {
			sm, err := repo.Manifests().Get(tag)
			if err != nil {
				ctxu.GetLogger(ctx).Fatalf("error getting manifest: %v", err)
			}

			dgst, err := digest.FromBytes(sm.Raw)
			if err != nil {
				ctxu.GetLogger(ctx).Fatalf("error digesting manifest: %v", err)
			}

			created := time.Time{}
			var size int64

			for _, fsLayer := range sm.FSLayers {
				layer, err := repo.Layers().Fetch(fsLayer.BlobSum)
				if err != nil {
					ctxu.GetLogger(ctx).Fatalf("error fetching layer: %v", err)
				}

				if created.Before(layer.CreatedAt()) {
					created = layer.CreatedAt()
				}

				ls, err := layer.Seek(0, os.SEEK_END)
				if err != nil {
					ctxu.GetLogger(ctx).Fatalf("error seeking layer: %v", err)
				}

				size += ls
			}

			fmt.Fprintf(wr, "%s\t%s\t%s\t%s\t%s\n", name, tag, dgst, humanize.Time(created), humanize.Bytes(uint64(size)))
		}
		wr.Flush()

		return nil
	})

}

func walk(driver driver.StorageDriver, path string, fn func(path string) error) error {
	fi, err := driver.Stat(path)
	if err != nil {
		return err
	}

	if !fi.IsDir() {
		return nil
	}

	entries, err := driver.List(path)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if err := fn(entry); err != nil {
			continue // just skip
		}

		if err := walk(driver, entry, fn); err != nil {
			return err
		}
	}

	return nil
}
