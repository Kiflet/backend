package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type LocalStorage struct {
	basePath string
	baseURL  string
	// root opens every file, so no name, link or race can reach outside
	// basePath. It stays open for the life of the process.
	root *os.Root
}

func NewLocalStorage(basePath string) (*LocalStorage, error) {
	if basePath == "" {
		basePath = "./uploads"
	}

	absPath, err := filepath.Abs(basePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	if err := os.MkdirAll(absPath, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}
	root, err := os.OpenRoot(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open storage directory: %w", err)
	}

	return &LocalStorage{
		basePath: absPath,
		baseURL:  "/uploads",
		root:     root,
	}, nil
}

func (l *LocalStorage) Upload(ctx context.Context, path string, reader io.Reader, contentType string) (string, error) {
	name, err := l.name(path)
	if err != nil {
		return "", err
	}
	if err := l.root.MkdirAll(filepath.Dir(name), 0o750); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	file, err := l.root.Create(name)
	if err != nil {
		return "", fmt.Errorf("failed to create file: %w", err)
	}
	// Closing a written file can fail too, when what was buffered can't be
	// written out: the file is then incomplete.
	_, err = io.Copy(file, reader)
	if cerr := file.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = l.root.Remove(name) // the error above is the one to report
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	return path, nil
}

func (l *LocalStorage) Download(ctx context.Context, path string) (io.ReadCloser, error) {
	name, err := l.name(path)
	if err != nil {
		return nil, err
	}

	file, err := l.root.Open(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrFileNotFound
		}
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	return file, nil
}

func (l *LocalStorage) Delete(ctx context.Context, path string) error {
	name, err := l.name(path)
	if err != nil {
		return err
	}

	if err := l.root.Remove(name); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrFileNotFound
		}
		return fmt.Errorf("failed to delete file: %w", err)
	}

	return nil
}

func (l *LocalStorage) GetURL(ctx context.Context, path string) (string, error) {
	if err := l.validatePath(path); err != nil {
		return "", err
	}

	return l.baseURL + "/" + strings.TrimPrefix(path, "/"), nil
}

func (l *LocalStorage) validatePath(path string) error {
	if path == "" || strings.Contains(path, "..") {
		return ErrInvalidPath
	}

	cleanPath := filepath.Clean(path)
	if strings.HasPrefix(cleanPath, "/") || strings.HasPrefix(cleanPath, "\\") {
		cleanPath = cleanPath[1:]
	}

	fullPath := filepath.Join(l.basePath, cleanPath)
	if !strings.HasPrefix(fullPath, l.basePath) {
		return ErrInvalidPath
	}

	return nil
}

// name is path as a name inside the root: checked, cleaned and relative.
func (l *LocalStorage) name(path string) (string, error) {
	if err := l.validatePath(path); err != nil {
		return "", err
	}
	return strings.TrimLeft(filepath.Clean(path), `/\`), nil
}

func (l *LocalStorage) GetBasePath() string {
	return l.basePath
}
