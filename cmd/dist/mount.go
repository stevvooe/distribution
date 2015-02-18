package main

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/codegangsta/cli"
	ctxu "github.com/docker/distribution/context"
	"github.com/docker/distribution/registry/storage"
	"github.com/docker/distribution/registry/storage/driver/filesystem"
	"golang.org/x/net/context"
)

var (
	commandMount = cli.Command{
		Name:   "mount",
		Usage:  "Mount the image at path",
		Action: mount,
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:  "r,registry",
				Value: "hub.docker.io",
				Usage: "Registry to use (e.g.: localhost:5000)",
			},
		},
	}
)

func mount(c *cli.Context) {
	ctx := context.Background()
	image := c.Args().First()
	mountPath := c.Args().Get(1)

	if mountPath == "" {
		ctxu.GetLogger(ctx).Fatalln("must specify mount path")
	}

	fi, err := os.Stat(mountPath)
	if err != nil {
		ctxu.GetLogger(ctx).Fatalln(err)
	}

	if !fi.IsDir() {
		ctxu.GetLogger(ctx).Fatalln("mount path should be a directory")
	}

	local := storage.NewRegistryWithDriver(filesystem.New("/tmp/local-registry"))
	name, tag, revision := splitNameTag(image)

	repo, err := local.Repository(ctx, name)
	if err != nil {
		ctxu.GetLogger(ctx).Fatalln(err)
	}

	sm, err := repo.Manifests().Get(tag)
	if err != nil {
		ctxu.GetLogger(ctx).Fatalln(err)
	}

	ls := repo.Layers()
	for i := len(sm.FSLayers) - 1; i >= 0; i-- {
		ctxu.GetLogger(ctx).Infof("unpack %v", sm.FSLayers[i].BlobSum)
		layer, err := ls.Fetch(sm.FSLayers[i].BlobSum)
		if err != nil {

			ctxu.GetLogger(ctx).Fatalln(err)
		}

		if err := extractTarFile(ctx, mountPath, layer); err != nil {
			ctxu.GetLogger(ctx).Infof("error extracting tar: %v", err)
		}
	}

	fmt.Println(mountPath, revision)
}

func extractTarFile(ctx context.Context, path string, rd io.Reader) error {
	cmd := exec.Command("tar", "-x", "-C", path) // may need some extra options for users/permissions
	cmd.Stdin = rd
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func writeTarFile(ctx context.Context, path string, hdr *tar.Header, rd io.Reader) error {
	target := filepath.Join(path, hdr.Name)

	switch hdr.Typeflag {
	case tar.TypeReg, tar.TypeRegA:
		fp, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		defer fp.Close()

		if _, err := io.Copy(fp, rd); err != nil {
			return err
		}

		fp.Chmod(hdr.FileInfo().Mode())
		// fp.Chmod(hdr.Uid, hdr.Gid)
		os.Chtimes(target, hdr.AccessTime, hdr.ModTime)
	case tar.TypeDir:
		if err := os.MkdirAll(target, hdr.FileInfo().Mode()); err != nil {
			return err
		}
	default:
		ctxu.GetLogger(ctx).Infof("skip %q %v -> %v", hdr.Typeflag, hdr.Name, hdr.Linkname)
		return fmt.Errorf("unsupported file: %v", hdr)
	}

	return nil
}

func splitNameTag(raw string) (name, tag, revision string) {
	name = raw
	if strings.Contains(name, "@") {
		parts := strings.Split(name, "@")
		if len(parts) != 2 {
			panic("bad name")
		}
		name = parts[0]
		revision = parts[1]
	}

	if strings.Contains(name, ":") {
		parts := strings.Split(name, ":")

		if len(parts) != 2 {
			panic("bad name")
		}

		name = parts[0]
		tag = parts[1]
	}

	return
}
