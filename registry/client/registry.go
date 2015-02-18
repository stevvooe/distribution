package client

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"time"

	ctxu "github.com/docker/distribution/context"

	"github.com/docker/distribution/manifest"

	"github.com/docker/distribution/digest"

	"github.com/docker/distribution"
	"github.com/docker/distribution/registry/api/v2"
	"golang.org/x/net/context"
)

type registry struct {
	client *http.Client
	ub     *v2.URLBuilder
}

func NewRegistryClient(client *http.Client, root string) (distribution.Registry, error) {
	ub, err := v2.NewURLBuilderFromString(root)
	if err != nil {
		return nil, err
	}

	return &registry{
		client: client,
		ub:     ub,
	}, nil
}

func (r *registry) Repository(ctx context.Context, name string) (distribution.Repository, error) {
	if err := v2.ValidateRespositoryName(name); err != nil {
		return nil, err
	}

	return &repository{
		name:     name,
		registry: r,
		context:  ctx,
	}, nil
}

type repository struct {
	*registry
	context context.Context
	name    string
}

func (r *repository) Name() string {
	return r.name
}

func (r *repository) Layers() distribution.LayerService {
	return &layers{
		repository: r,
	}
}

func (r *repository) Manifests() distribution.ManifestService {
	return &manifests{
		repository: r,
	}
}

type manifests struct {
	*repository
}

func (ms *manifests) Tags() ([]string, error) {
	panic("not implemented")
}

func (ms *manifests) Exists(tag string) (bool, error) {
	panic("not implemented")
}

func (ms *manifests) Get(tag string) (*manifest.SignedManifest, error) {
	u, err := ms.ub.BuildManifestURL(ms.name, tag)
	if err != nil {
		return nil, err
	}

	resp, err := ms.client.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var sm manifest.SignedManifest
	switch {
	case resp.StatusCode == 200:
		decoder := json.NewDecoder(resp.Body)

		if err := decoder.Decode(&sm); err != nil {
			return nil, err
		}

		return &sm, nil
	}

	panic("bad get")
}

func (ms *manifests) Put(tag string, m *manifest.SignedManifest) error {
	manifestURL, err := ms.ub.BuildManifestURL(ms.name, tag)
	if err != nil {
		return err
	}

	putRequest, err := http.NewRequest("PUT", manifestURL, bytes.NewReader(m.Raw))
	if err != nil {
		return err
	}

	response, err := http.DefaultClient.Do(putRequest)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	switch {
	case response.StatusCode == http.StatusOK || response.StatusCode == http.StatusAccepted:
		return nil
	case response.StatusCode >= 400 && response.StatusCode < 500:
		var errors v2.Errors
		decoder := json.NewDecoder(response.Body)
		err = decoder.Decode(&errors)
		if err != nil {
			return err
		}

		return &errors
	default:
		return &UnexpectedHTTPStatusError{Status: response.Status}
	}
}

func (ms *manifests) Delete(tag string) error {
	panic("manifest delete implemented")
}

type layers struct {
	*repository
}

func (ls *layers) Exists(dgst digest.Digest) (bool, error) {
	_, err := ls.fetchLayer(dgst)
	if err != nil {
		switch err := err.(type) {
		case distribution.ErrUnknownLayer:
			return false, nil
		default:
			return false, err
		}
	}

	return true, nil
}

func (ls *layers) Fetch(dgst digest.Digest) (distribution.Layer, error) {
	return ls.fetchLayer(dgst)
}

func (ls *layers) Upload() (distribution.LayerUpload, error) {
	u, err := ls.ub.BuildBlobUploadURL(ls.name)

	resp, err := ls.client.Post(u, "", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusAccepted:
		location := resp.Header.Get("Location")

		// TODO(stevvooe): Add helper function to v2 package to make this
		// easier. This url format dependent.
		u, err := url.Parse(location)
		if err != nil {
			return nil, err
		}

		uuid := path.Base(u.Path)

		return &httpLayerUpload{
			layers:    ls,
			uuid:      uuid,
			startedAt: time.Now(),
			location:  location,
		}, nil
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		var errs v2.Errors
		decoder := json.NewDecoder(resp.Body)
		err = decoder.Decode(&errs)
		if err != nil {
			return nil, err
		}
		return nil, &errs
	default:
		return nil, &UnexpectedHTTPStatusError{Status: resp.Status}
	}
}

func (ls *layers) Resume(uuid string) (distribution.LayerUpload, error) {
	panic("not implemented")
}

func (ls *layers) fetchLayer(dgst digest.Digest) (distribution.Layer, error) {
	u, err := ls.ub.BuildBlobURL(ls.name, dgst)
	if err != nil {
		return nil, err
	}

	resp, err := ls.client.Head(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		lengthHeader := resp.Header.Get("Content-Length")
		length, err := strconv.ParseInt(lengthHeader, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("error parsing content-length: %v", err)
		}

		t, err := http.ParseTime(resp.Header.Get("Last-Modified"))
		if err != nil {
			return nil, fmt.Errorf("error parsing last-modified: %v", err)
		}

		return &httpLayer{
			layers:    ls,
			size:      length,
			digest:    dgst,
			createdAt: t,
		}, nil
	case resp.StatusCode == http.StatusNotFound:
		return nil, distribution.ErrUnknownLayer{
			FSLayer: manifest.FSLayer{
				BlobSum: dgst,
			},
		}
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		var errs v2.Errors
		decoder := json.NewDecoder(resp.Body)
		err = decoder.Decode(&errs)
		if err != nil {
			return nil, err
		}
		return nil, &errs
	default:
		return nil, &UnexpectedHTTPStatusError{Status: resp.Status}
	}
}

type httpLayer struct {
	*layers

	size      int64
	digest    digest.Digest
	createdAt time.Time

	rc     io.ReadCloser // remote read closer
	brd    *bufio.Reader // internal buffered io
	offset int64
	err    error
}

func (hl *httpLayer) CreatedAt() time.Time {
	return hl.createdAt
}

func (hl *httpLayer) Digest() digest.Digest {
	return hl.digest
}

func (hl *httpLayer) Read(p []byte) (n int, err error) {
	if hl.err != nil {
		return 0, hl.err
	}

	rd, err := hl.reader()
	if err != nil {
		return 0, err
	}

	n, err = rd.Read(p)
	hl.offset += int64(n)

	// Simulate io.EOR error if we reach filesize.
	if err == nil && hl.offset >= hl.size {
		err = io.EOF
	}

	return n, err
}

func (hl *httpLayer) Seek(offset int64, whence int) (int64, error) {
	if hl.err != nil {
		return 0, hl.err
	}

	var err error
	newOffset := hl.offset

	switch whence {
	case os.SEEK_CUR:
		newOffset += int64(offset)
	case os.SEEK_END:
		newOffset = hl.size + int64(offset)
	case os.SEEK_SET:
		newOffset = int64(offset)
	}

	if newOffset < 0 {
		err = fmt.Errorf("cannot seek to negative position")
	} else {
		if hl.offset != newOffset {
			hl.reset()
		}

		// No problems, set the offset.
		hl.offset = newOffset
	}

	return hl.offset, err
}

func (hl *httpLayer) Close() error {
	if hl.err != nil {
		return hl.err
	}

	// close and release reader chain
	if hl.rc != nil {
		hl.rc.Close()
	}

	hl.rc = nil
	hl.brd = nil

	hl.err = fmt.Errorf("httpLayer: closed")

	return nil
}

func (hl *httpLayer) reset() {
	if hl.err != nil {
		return
	}
	if hl.rc != nil {
		hl.rc.Close()
		hl.rc = nil
	}
}

func (hl *httpLayer) reader() (io.Reader, error) {
	if hl.err != nil {
		return nil, hl.err
	}

	if hl.rc != nil {
		return hl.brd, nil
	}

	// If the offset is great than or equal to size, return a empty, noop reader.
	if hl.offset >= hl.size {
		return ioutil.NopCloser(bytes.NewReader([]byte{})), nil
	}

	blobURL, err := hl.ub.BuildBlobURL(hl.name, hl.digest)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("GET", blobURL, nil)
	if err != nil {
		return nil, err
	}

	if hl.offset > 0 {
		// TODO(stevvooe): Get this working correctly.

		// If we are at different offset, issue a range request from there.
		req.Header.Add("Range", fmt.Sprintf("1-"))
		ctxu.GetLogger(hl.context).Infof("Range: %s", req.Header.Get("Range"))
	}

	resp, err := hl.client.Do(req)
	if err != nil {
		return nil, err
	}

	switch {
	case resp.StatusCode == 200:
		hl.rc = resp.Body
	default:
		defer resp.Body.Close()
		return nil, fmt.Errorf("unexpected status resolving reader: %v", resp.Status)
	}

	if hl.brd == nil {
		hl.brd = bufio.NewReader(hl.rc)
	} else {
		hl.brd.Reset(hl.rc)
	}

	return hl.brd, nil
}

type httpLayerUpload struct {
	*layers

	uuid      string
	startedAt time.Time

	location string // always the last value of the location header.
	buf      bytes.Buffer
	closed   bool
}

var _ distribution.LayerUpload = &httpLayerUpload{}

func (hlu *httpLayerUpload) ReadFrom(r io.Reader) (n int64, err error) {
	return hlu.buf.ReadFrom(r)
}

func (hlu *httpLayerUpload) Write(p []byte) (n int, err error) {
	return hlu.buf.Write(p)
}

func (hlu *httpLayerUpload) Seek(offset int64, whence int) (int64, error) {
	panic("not implemented")
}

func (hlu *httpLayerUpload) UUID() string {
	panic("not implemented")
}

func (hlu *httpLayerUpload) StartedAt() time.Time {
	panic("not implemented")
}

func (hlu *httpLayerUpload) Finish(digest digest.Digest) (distribution.Layer, error) {
	// TODO(stevvooe): This is really bad: all the data is flushed in a single request.
	req, err := http.NewRequest("PUT", hlu.location, bytes.NewReader(hlu.buf.Bytes()))
	if err != nil {
		return nil, err
	}
	defer req.Body.Close()

	values := req.URL.Query()
	values.Set("digest", digest.String())
	req.URL.RawQuery = values.Encode()

	resp, err := hlu.client.Do(req)
	if err != nil {
		return nil, err
	}

	switch {
	case resp.StatusCode == http.StatusCreated:
		return hlu.Layers().Fetch(digest)
	case resp.StatusCode == http.StatusNotFound:
		return nil, &BlobUploadNotFoundError{Location: hlu.location}
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		var errs v2.Errors
		decoder := json.NewDecoder(resp.Body)
		err = decoder.Decode(&errs)
		if err != nil {
			return nil, err
		}
		return nil, &errs
	default:
		return nil, &UnexpectedHTTPStatusError{Status: resp.Status}
	}
}

func (hlu *httpLayerUpload) Cancel() error {
	panic("not implemented")
}

func (hlu *httpLayerUpload) Close() error {
	hlu.closed = true
	return nil
}
