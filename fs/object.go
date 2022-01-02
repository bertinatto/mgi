package fs

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/bertinatto/mgi"
)

type ObjectService struct {
	path string
}

func NewObjectService(root string) *ObjectService {
	return &ObjectService{
		path: filepath.Join(root, "objects"),
	}
}

// TODO: header and data should be of the same type
func (o *ObjectService) store(header string, data []byte) (string, [20]byte, error) {
	// Set up the content
	c := new(bytes.Buffer)
	c.Grow(len(header) + len(data))
	c.WriteString(header)
	c.Write(data)
	content := c.Bytes()

	// Calculate the SHA-1 hash
	hash := fmt.Sprintf("%x", sha1.Sum(content))
	hash1 := sha1.Sum(content)

	// Create directory
	dir := filepath.Join(o.path, string(hash[:2]))
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return "", [20]byte{}, err
	}

	// Create a buffer containing the zlib-compressed content
	b := new(bytes.Buffer)
	w := zlib.NewWriter(b)
	_, err = w.Write(content)
	if err != nil {
		return "", [20]byte{}, err
	}
	w.Close()

	// Create file and store compressed data
	obj := filepath.Join(dir, string(hash[2:]))
	err = ioutil.WriteFile(obj, b.Bytes(), 0755)
	if err != nil {
		return "", [20]byte{}, err
	}
	return hash, hash1, nil

}

func (o *ObjectService) Store(objType string, data []byte) (string, [20]byte, error) {
	switch objType {
	case mgi.BlobType:
		// Set up the header
		header := fmt.Sprintf("blob %d\x00", len(data))
		return o.store(header, data)
	case mgi.TreeType:
		// Set up the header
		header := fmt.Sprintf("tree %d\x00", len(data))
		return o.store(header, data)
	case mgi.CommitType:
		// Set up the header
		header := fmt.Sprintf("commit %d\x00", len(data))
		return o.store(header, data)
	default:
		return "", [20]byte{}, fmt.Errorf("unknown object type: %s", objType)
	}
}

func (o *ObjectService) Read(hash string) (string, []byte, error) {
	dir := hash[:2]
	obj := hash[2:]
	path := filepath.Join(o.path, dir, obj)
	f, err := os.Open(path)
	if err != nil {
		return "", nil, err
	}

	r, err := zlib.NewReader(f)
	if err != nil {
		return "", nil, err
	}

	content, err := ioutil.ReadAll(r)
	if err != nil {
		return "", nil, err
	}

	return path, content, nil
}
