package mgi

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"time"
)

// Hash represents a SHA-1 signature.
type Hash struct {
	sha1 [20]byte
}

// From returns a new *Hash from an existing SHA-1.
func (h *Hash) FromSHA1(sha1 [20]byte) *Hash {
	h.sha1 = sha1
	return h
}

// From returns a new *Hash from a given file.
func (h *Hash) From(data []byte) *Hash {
	h.sha1 = sha1.Sum(data)
	return h
}

// String returns a string representing the SHA-1 sum.
func (h *Hash) String() string {
	return fmt.Sprintf("%x", h.sha1)
}

// Bytes returns a slice representing the SHA-1 sum.
func (h *Hash) Bytes() []byte {
	return h.sha1[:]
}

// Sha1 returns a 20 bytes long array of bytes representing the SHA-1 sum.
func (h *Hash) Sha1() [20]byte {
	return h.sha1
}

// Marshaller is the interface that object needs to implement in order to be stored.
type Marshaller interface {
	// Marshal serializes the object into an slice of bytes with all the metadata required.
	Marshal() ([]byte, error)
}

// Blob represents a file.
type Blob struct {
	Data []byte
}

func (b *Blob) Marshal() ([]byte, error) {
	header := []byte(fmt.Sprintf("blob %d\x00", len(b.Data)))
	return join(header, b.Data)
}

// TreeEntry represents a single entry in the tree
type TreeEntry struct {
	mode uint32
	path string
	sha1 [20]byte
}

// Tree represents a directory with potentially other directories or files.
type Tree struct {
	Entries []*TreeEntry
}

func (t *Tree) Marshal() ([]byte, error) {
	b := new(bytes.Buffer)
	for _, e := range t.Entries {
		b.WriteString(fmt.Sprintf("%o %s\x00%s", e.mode, e.path, e.sha1))
	}
	data := b.Bytes()
	header := []byte(fmt.Sprintf("tree %d\x00", len(data)))
	return join(header, data)
}

// Commit represents a commit object.
type Commit struct {
	Parent      string
	Tree        string
	Author      string
	AuthorEmail string
	AuthorTime  time.Time
	Message     string
}

func (c *Commit) Marshal() ([]byte, error) {
	// Add the "tree xxx" line
	b := new(bytes.Buffer)
	b.WriteString("tree ")
	b.WriteString(c.Tree)
	b.WriteString("\n")

	// Add the "parent xxx" line
	if len(c.Parent) > 0 {
		b.WriteString("parent ")
		b.WriteString(c.Parent)
		b.WriteString("\n")
	}

	// Find out the author time
	_, offset := c.AuthorTime.Zone()
	var sign string
	if offset > 0 {
		sign = "+"
	} else {
		sign = "-"
	}
	fo := int64(math.Abs(float64(offset)))
	timestamp := int64(math.Abs(float64(c.AuthorTime.Unix())))
	authorTime := fmt.Sprintf("%d %s%02d%02d", timestamp, sign, fo/3600, (fo/60)%60)

	// Add the "author/commit xxx" line
	b.WriteString(fmt.Sprintf("author %s <%s> %s", c.Author, c.AuthorEmail, authorTime))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("committer %s <%s> %s", c.Author, c.AuthorEmail, authorTime))
	b.WriteString("\n")
	b.WriteString("\n")
	b.WriteString(c.Message)
	b.WriteString("\n")

	data := b.Bytes()
	header := []byte(fmt.Sprintf("commit %d\x00", len(data)))
	return join(header, data)
}

// ObjectService allows for storing objects to a given location.
type ObjectService struct {
	path string
}

// NewObjectService creates a new ObjectService.
func NewObjectService(root string) *ObjectService {
	return &ObjectService{
		path: filepath.Join(root, "objects"),
	}
}

func (o *ObjectService) HashObject(m Marshaller) (*Hash, error) {
	data, err := m.Marshal()
	if err != nil {
		return nil, err
	}
	return new(Hash).From(data), nil
}

// StoreObject compresses and stores the object to the disk.
func (o *ObjectService) StoreObject(m Marshaller) (*Hash, error) {
	data, err := m.Marshal()
	if err != nil {
		return nil, err
	}

	// Calculate the SHA-1 hash of the object
	hash := new(Hash).From(data)
	hashStr := hash.String()

	// Create a buffer containing the zlib-compressed content
	zData := new(bytes.Buffer)
	w := zlib.NewWriter(zData)
	_, err = w.Write(data)
	if err != nil {
		return nil, err
	}
	w.Close()

	// Create directory
	dir := filepath.Join(o.path, string(hashStr[:2]))
	err = os.MkdirAll(dir, 0755)
	if err != nil {
		return nil, err
	}

	// Create a file out of the compressed data
	obj := filepath.Join(dir, string(hashStr[2:]))
	return hash, ioutil.WriteFile(obj, zData.Bytes(), 0755)
}

// ReadObject reads the object from disk, uncompress and returns its contents.
func (o *ObjectService) ReadObject(hash string) ([]byte, error) {
	path := filepath.Join(o.path, hash[:2], hash[2:])
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r, err := zlib.NewReader(f)
	if err != nil {
		return nil, err
	}

	contents, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}

	i := bytes.IndexByte(contents, byte('\x00'))
	return contents[i+1:], nil
}

func join(header []byte, data []byte) ([]byte, error) {
	content := new(bytes.Buffer)
	content.Grow(len(header) + len(data))
	content.Write(header)
	content.Write(data)
	return content.Bytes(), nil
}
