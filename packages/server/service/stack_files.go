package service

import (
	"fmt"
	"io"
	"os"
)

// maxRepoRead caps any single read from a checkout.
const maxRepoRead = 4 << 20

// readRepoFile reads p from a checkout through os.Root, so neither the path nor
// a symlink in the repository can reach a file outside it, such as the API's
// own configuration.
func readRepoFile(dir, p string) ([]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxRepoRead+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxRepoRead {
		return nil, fmt.Errorf("%s is larger than %d MiB", p, maxRepoRead>>20)
	}
	return data, nil
}
