package distribution

import (
	"io"

	"golang.org/x/net/context"
)

func Pull(ctx context.Context, dst, src Registry, name, tag string) error {
	srcRepo, err := src.Repository(ctx, name)
	if err != nil {
		return err
	}

	sm, err := srcRepo.Manifests().Get(tag)
	if err != nil {
		return err
	}

	dstRepo, err := dst.Repository(ctx, name)
	if err != nil {
		return err
	}

	srcLS := srcRepo.Layers()
	dstLS := dstRepo.Layers()
	for _, fsLayer := range sm.FSLayers {
		layer, err := srcLS.Fetch(fsLayer.BlobSum)
		if err != nil {
			return err
		}

		upload, err := dstLS.Upload()
		if err != nil {
			return err
		}

		if _, err := io.Copy(upload, layer); err != nil {
			return err
		}

		if _, err := upload.Finish(layer.Digest()); err != nil {
			return err
		}

		upload.Close()
	}

	if err := dstRepo.Manifests().Put(tag, sm); err != nil {
		return err
	}

	return nil
}

func Push(ctx context.Context, dst, src Registry, name, tag string) error {
	return Pull(ctx, dst, src, name, tag)
}
