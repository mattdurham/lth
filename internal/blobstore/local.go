// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package blobstore

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
)

// LocalStore implements BlobStore using the local filesystem.
// Keys are mapped to file paths under baseDir, with "/" treated as path separators.

// NewLocalStore creates a LocalStore rooted at baseDir.
// baseDir is created if it does not exist.
func NewLocalStore(baseDir string) (*LocalStore, error) {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("create base dir: %w", err)
	}
	abs, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, fmt.Errorf("resolve base dir: %w", err)
	}
	return &LocalStore{baseDir: abs}, nil
}

func (s *LocalStore) keyToPath(key string) (string, error) {
	// Clean slashes to OS separator and resolve absolute path.
	rel := filepath.FromSlash(key)
	full := filepath.Clean(filepath.Join(s.baseDir, rel))
	if !strings.HasPrefix(full, s.baseDir+string(os.PathSeparator)) && full != s.baseDir {
		return "", fmt.Errorf("key %q escapes base directory", key)
	}
	return full, nil
}

func (s *LocalStore) Put(ctx context.Context, key string, r io.Reader) error {
	target, err := s.keyToPath(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	tmp := filepath.Join(filepath.Dir(target), "."+uuid.NewString()+".tmp")
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("write: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmp, target); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

func (s *LocalStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	target, err := s.keyToPath(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(target)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("key %q: %w", key, fs.ErrNotExist)
		}
		return nil, fmt.Errorf("open: %w", err)
	}
	return f, nil
}

func (s *LocalStore) Exists(ctx context.Context, key string) (bool, error) {
	target, err := s.keyToPath(key)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(target)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("stat: %w", err)
}

func (s *LocalStore) Delete(ctx context.Context, key string) error {
	target, err := s.keyToPath(key)
	if err != nil {
		return err
	}
	err = os.Remove(target)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove: %w", err)
	}
	return nil
}

func (s *LocalStore) List(ctx context.Context, prefix string) ([]BlobObject, error) {
	var results []BlobObject
	err := filepath.WalkDir(s.baseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(s.baseDir, path)
		if relErr != nil {
			return relErr
		}
		key := filepath.ToSlash(rel)
		if prefix == "" || strings.HasPrefix(key, prefix) {
			info, infoErr := d.Info()
			if infoErr != nil {
				return infoErr
			}
			results = append(results, BlobObject{
				Key:          key,
				Size:         info.Size(),
				LastModified: info.ModTime(),
			})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk: %w", err)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Key < results[j].Key
	})
	if results == nil {
		results = []BlobObject{}
	}
	return results, nil
}
