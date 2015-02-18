package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/docker/distribution/registry/api/v2"

	"github.com/codegangsta/cli"
	ctxu "github.com/docker/distribution/context"
	"github.com/docker/distribution/digest"
	"github.com/docker/distribution/registry/storage"
	"github.com/docker/distribution/registry/storage/driver/filesystem"
	"golang.org/x/net/context"
)

var (
	commandSnapshot = cli.Command{
		Name:   "snapshot",
		Usage:  "Snapshot the desired directory.",
		Action: snapshot,
		Flags:  []cli.Flag{},
	}
)

func snapshot(c *cli.Context) {
	ctx := context.Background()
	local := storage.NewRegistryWithDriver(filesystem.New("/tmp/local-registry"))
	name := c.Args().Get(0)
	path := c.Args().Get(1)

	if err := v2.ValidateRespositoryName(name); err != nil {
		ctxu.GetLogger(ctx).Fatalf("provided repository name not valid: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		ctxu.GetLogger(ctx).Fatalf("error stating path: %v", err)
	}

	if !fi.IsDir() {
		ctxu.GetLogger(ctx).Fatalf("path must be a directory: %v", err)
	}

	ctxu.GetLogger(ctx).Infoln("snapshot", path)
	rc, err := tarDirectory(path)
	if err != nil {
		ctxu.GetLogger(ctx).Fatalf("error tarring directory: %v", err)
	}
	defer rc.Close()

	repo, err := local.Repository(ctx, name)
	if err != nil {
		ctxu.GetLogger(ctx).Fatalf(": %v", err)
	}

	upload, err := repo.Layers().Upload()
	if err != nil {
		ctxu.GetLogger(ctx).Fatalf("error creating upload: %v", err)
	}

	tr := io.TeeReader(rc, upload)
	dgst, err := digest.FromTarArchive(tr)
	if err != nil {
		upload.Cancel()
		ctxu.GetLogger(ctx).Fatalf("error digesting: %v", err)
	}

	layer, err := upload.Finish(dgst)
	if err != nil {
		ctxu.GetLogger(ctx).Fatalf("error finishing upload: %v", err)
	}
	defer layer.Close()

	fmt.Printf("%s@%s\n", name, layer.Digest())
}

type waitingReadCloser struct {
	io.ReadCloser
	cmd *exec.Cmd
}

func (wrc *waitingReadCloser) Close() error {
	if err := wrc.cmd.Wait(); err != nil {
		return err
	}

	return nil
}

func tarDirectory(path string) (io.ReadCloser, error) {
	cmd := exec.Command("tar", "-c", ".")
	cmd.Dir = path
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return stdout, nil
}
