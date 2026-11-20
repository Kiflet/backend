package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/kiflet/backend/pkg/storage"
)

// MediaService stores proof-of-delivery images. Files are addressed by a
// storage key recorded on the POD and only ever served through handlers
// that check the viewer may see that shipment.
type MediaService struct {
	*core
	store storage.Storage
	pod   *storage.PODStorage
}

func podStorage(st storage.Storage) *storage.PODStorage {
	if st == nil {
		return nil
	}
	return storage.NewPODStorage(st)
}

// Limits on what a device may upload. Photos arrive already reduced to
// 800×600 by the app; these bounds leave room for older clients.
const (
	maxSignatureBytes = 512 << 10
	maxPhotoBytes     = 4 << 20
)

// ErrMediaInvalid marks upload data that will never be accepted, as opposed
// to a storage failure worth retrying.
var ErrMediaInvalid = &Error{Kind: KindInvalid, Field: "photos", Message: "A photo or the signature could not be read."}

// ErrMediaTooLarge is a photo or signature with more pixels than is ever
// needed, refused before it is decoded.
var ErrMediaTooLarge = &Error{Kind: KindInvalid, Field: "photos", Message: "A photo or the signature is too large. Take the photo again, or at a lower resolution."}

// PODFile is a stored file of a proof of delivery, the signature or a
// photo, and when and where it was taken if the app said (pod_media).
type PODFile struct {
	storage.File
	TakenAt  time.Time
	Lat, Lng *float64
}

// SavePOD stores a signature and photos given as data URLs, and describes
// what it stored. The PWA doesn't say when or where each was taken.
func (m *MediaService) SavePOD(ctx context.Context, shipmentID, signature string, photos []string) (PODFile, []PODFile, error) {
	if m.pod == nil {
		return PODFile{}, nil, errors.New("media storage is not configured")
	}
	if signature == "" {
		return PODFile{}, nil, invalid("signature", "The recipient's signature is required.")
	}
	if len(signature) > maxSignatureBytes {
		return PODFile{}, nil, ErrMediaInvalid
	}
	if len(photos) > storage.MaxPhotosPerPOD {
		return PODFile{}, nil, invalid("photos", fmt.Sprintf("Attach at most %d photos.", storage.MaxPhotosPerPOD))
	}
	for _, p := range photos {
		if len(p) > maxPhotoBytes {
			return PODFile{}, nil, ErrMediaInvalid
		}
	}
	sig, err := m.pod.UploadSignature(ctx, shipmentID, signature)
	if err != nil {
		return PODFile{}, nil, classifyUpload(err)
	}
	stored, err := m.pod.UploadMultiplePhotos(ctx, shipmentID, photos)
	if err != nil {
		m.Delete(context.WithoutCancel(ctx), sig.Key)
		return PODFile{}, nil, classifyUpload(err)
	}
	files := make([]PODFile, len(stored))
	for i, f := range stored {
		files[i] = PODFile{File: f}
	}
	return PODFile{File: sig}, files, nil
}

// Delete removes stored files whose proof of delivery was refused.
func (m *MediaService) Delete(ctx context.Context, keys ...string) {
	if m.pod != nil {
		m.pod.Delete(ctx, keys...)
	}
}

func classifyUpload(err error) error {
	if errors.Is(err, storage.ErrImageTooLarge) {
		return ErrMediaTooLarge
	}
	msg := err.Error()
	if errors.Is(err, storage.ErrInvalidImageData) || errors.Is(err, storage.ErrInvalidSignature) ||
		strings.Contains(msg, "decode") {
		return ErrMediaInvalid
	}
	return err
}

// Open streams a stored file and reports its content type.
func (m *MediaService) Open(ctx context.Context, key string) (io.ReadCloser, string, error) {
	if m.store == nil {
		return nil, "", notFound("File not found.")
	}
	rc, err := m.store.Download(ctx, key)
	if errors.Is(err, storage.ErrFileNotFound) {
		return nil, "", notFound("File not found.")
	}
	if err != nil {
		return nil, "", err
	}
	ct := "application/octet-stream"
	switch strings.ToLower(path.Ext(key)) {
	case ".jpg", ".jpeg":
		ct = "image/jpeg"
	case ".png":
		ct = "image/png"
	case ".svg":
		ct = "image/svg+xml"
	case ".pdf":
		ct = "application/pdf"
	}
	return rc, ct, nil
}
