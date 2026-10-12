package clipboard

import (
	"errors"
	"os"
)

type Clipboard struct {
	path string
}

func New() *Clipboard { return &Clipboard{} }

func (c *Clipboard) Copy(data []byte) (string, error) {
	if c.path == "" {
		file, err := os.CreateTemp(os.TempDir(), "bc-clip-")
		if err != nil {
			return "", err
		}
		c.path = file.Name()
		if err := file.Close(); err != nil {
			return "", err
		}
	}
	file, err := os.OpenFile(c.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return "", err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	return c.path, nil
}

func (c *Clipboard) Paste() ([]byte, error) {
	if c.path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(c.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return data, err
}

func (c *Clipboard) Path() string     { return c.path }
func (c *Clipboard) Describe() string { return c.path }

func (c *Clipboard) Close() error {
	if c.path == "" {
		return nil
	}
	err := os.Remove(c.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
