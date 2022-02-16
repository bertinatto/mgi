package mgi

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"syscall"
)

// Index represents a git index file, typically ".git/index".
type Index struct {
	Signature  string
	Version    string
	EntryCount int
	Entries    []*IndexEntry
	Hash       *Hash
}

// IndexEntry stores
type IndexEntry struct {
	CTimeSecs     uint32
	CTimeNanoSecs uint32
	MTimeSecs     uint32
	MTimeNanoSecs uint32
	Dev           uint32
	Ino           uint32
	Mode          uint32
	Uid           uint32
	Gid           uint32
	FileSize      uint32
	Hash          *Hash
	Flags         uint16
	Path          string
}

type IndexService struct {
	path  string
	index *Index
}

func NewIndexService(root string) *IndexService {
	return &IndexService{
		path: filepath.Join(root, "index"),
		index: &Index{
			Signature: "DIRC",
			Version:   "2",
		},
	}
}

func (i *IndexService) Add(path string, hash *Hash) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	stat := fi.Sys().(*syscall.Stat_t)

	var mode uint32
	if stat.Mode&syscall.S_IXUSR != 0 {
		mode = 0100755
	} else {
		mode = 0100644
	}

	entry := &IndexEntry{
		CTimeSecs:     uint32(stat.Ctim.Sec),
		CTimeNanoSecs: uint32(stat.Ctim.Nsec),
		MTimeSecs:     uint32(stat.Mtim.Sec),
		MTimeNanoSecs: uint32(stat.Mtim.Nsec),
		Dev:           uint32(stat.Dev),
		Ino:           uint32(stat.Ino),
		Mode:          mode,
		Uid:           stat.Uid,
		Gid:           stat.Gid,
		FileSize:      uint32(stat.Size),
		Hash:          hash,
		Flags:         uint16(len(path)),
		Path:          path,
	}

	var replaced bool
	for ei, v := range i.index.Entries {
		if v.Path == entry.Path {
			i.index.Entries[ei] = entry
			replaced = true
		}
	}

	if !replaced {
		i.index.Entries = append(i.index.Entries, entry)
		i.index.EntryCount = len(i.index.Entries)
	}

	return nil
}

func (i *IndexService) Marshal() ([]byte, error) {
	// Build the index signature.
	var signature [4]byte
	if len(i.index.Signature) != 4 {
		return nil, fmt.Errorf("signature must be 4 bytes long")
	}
	copy(signature[:], []byte(i.index.Signature))
	mb := new(bytes.Buffer)
	binary.Write(mb, binary.BigEndian, signature)

	// Build version and entry count.
	version, err := strconv.Atoi(i.index.Version)
	if err != nil {
		return nil, err
	}
	binary.Write(mb, binary.BigEndian, uint32(version))
	binary.Write(mb, binary.BigEndian, uint32(i.index.EntryCount))

	// Index entries should be sorted
	sort.Slice(i.index.Entries, func(x, y int) bool {
		return i.index.Entries[x].Path < i.index.Entries[y].Path
	})

	// Serialize each index entry to a format that can be stored on disk.
	for _, v := range i.index.Entries {
		b := new(bytes.Buffer)
		binary.Write(b, binary.BigEndian, v.CTimeSecs)
		binary.Write(b, binary.BigEndian, v.CTimeNanoSecs)
		binary.Write(b, binary.BigEndian, v.MTimeSecs)
		binary.Write(b, binary.BigEndian, v.MTimeNanoSecs)
		binary.Write(b, binary.BigEndian, v.Dev)
		binary.Write(b, binary.BigEndian, v.Ino)
		binary.Write(b, binary.BigEndian, v.Mode)
		binary.Write(b, binary.BigEndian, v.Uid)
		binary.Write(b, binary.BigEndian, v.Gid)
		binary.Write(b, binary.BigEndian, v.FileSize)
		binary.Write(b, binary.BigEndian, v.Hash)
		binary.Write(b, binary.BigEndian, v.Flags)
		b.WriteString(v.Path)
		b.WriteString("\x00")

		// Some entries require some padding.
		length := ((62 + len(v.Path) + 8) / 8) * 8
		for b.Len() < length {
			b.WriteString("\x00")
		}
		mb.Write(b.Bytes())
	}

	binary.Write(mb, binary.BigEndian, sha1.Sum(mb.Bytes()))
	return mb.Bytes(), nil
}

func (i *IndexService) Store() error {
	data, err := i.Marshal()
	if err != nil {
		return err
	}
	fd, err := os.OpenFile(i.path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer fd.Close()
	_, err = fd.Write(data)
	return err
}

func (i *IndexService) Read() (*Index, error) {
	// Read the file stored on disk and create an in-memory representation of it.
	data, err := ioutil.ReadFile(i.path)
	if errors.Is(err, os.ErrNotExist) {
		return &Index{
			Signature:  "DIRC",
			Version:    "2",
			EntryCount: 0,
			Entries:    nil,
			Hash:       nil, // This will be set once the file is writtten to disk
		}, nil
	}
	if err != nil {
		return nil, err
	}

	// Pre-populate header.
	i.index.Signature = string(data[:4])
	i.index.Version = fmt.Sprintf("%d", binary.BigEndian.Uint32(data[4:8]))
	i.index.EntryCount = int(binary.BigEndian.Uint32(data[8:12]))
	i.index.Hash = new(Hash).FromSHA1Bytes(data[len(data)-20:])

	if i.index.Version != "2" {
		return nil, fmt.Errorf("unsupported version %q", i.index.Version)
	}

	payloadHash := new(Hash).From(data[:len(data)-20])
	if i.index.Hash.String() != payloadHash.String() {
		return nil, fmt.Errorf("digests don't match")
	}

	// Create a reader for the useful area of the buffer. The first 12 bytes are
	// reserved for the header (signature, version, entry count) and the last 20
	// bytes are reserved for the digest.
	reader := bufio.NewReader(bytes.NewReader(data[12 : len(data)-20]))

	// Read entries stored on disk and convert them to the in-memory representation.
	entries := make([]*IndexEntry, 0, i.index.EntryCount)
	for idx := 0; idx < i.index.EntryCount; idx++ {
		e := new(IndexEntry)

		v, err := readNBytes(reader, 4)
		if err != nil {
			return nil, err
		}
		e.CTimeSecs = binary.BigEndian.Uint32(v)

		v, err = readNBytes(reader, 4)
		if err != nil {
			return nil, err
		}
		e.CTimeNanoSecs = binary.BigEndian.Uint32(v)

		v, err = readNBytes(reader, 4)
		if err != nil {
			return nil, err
		}
		e.MTimeSecs = binary.BigEndian.Uint32(v)

		v, err = readNBytes(reader, 4)
		if err != nil {
			return nil, err
		}
		e.MTimeNanoSecs = binary.BigEndian.Uint32(v)

		v, err = readNBytes(reader, 4)
		if err != nil {
			return nil, err
		}
		e.Dev = binary.BigEndian.Uint32(v)

		v, err = readNBytes(reader, 4)
		if err != nil {
			return nil, err
		}
		e.Ino = binary.BigEndian.Uint32(v)

		v, err = readNBytes(reader, 4)
		if err != nil {
			return nil, err
		}
		e.Mode = binary.BigEndian.Uint32(v)

		v, err = readNBytes(reader, 4)
		if err != nil {
			return nil, err
		}
		e.Uid = binary.BigEndian.Uint32(v)

		v, err = readNBytes(reader, 4)
		if err != nil {
			return nil, err
		}
		e.Gid = binary.BigEndian.Uint32(v)

		v, err = readNBytes(reader, 4)
		if err != nil {
			return nil, err
		}
		e.FileSize = binary.BigEndian.Uint32(v)

		v, err = readNBytes(reader, 20)
		if err != nil {
			return nil, err
		}
		e.Hash = new(Hash).FromSHA1Bytes(v)

		v, err = readNBytes(reader, 2)
		if err != nil {
			return nil, err
		}
		e.Flags = binary.BigEndian.Uint16(v)

		path, err := reader.ReadBytes('\x00')
		if err != nil {
			return nil, err
		}
		// ReadBytes returns the \x00 byte as well, so we need to pop it out.
		e.Path = string(path[:len(path)-1])

		// We need to take into account the padding bytes to point the index variable to the right location.
		totalEntryLen := ((62 + len(e.Path) + 1 /* this is the \x00 byte */ + 8) / 8) * 8
		padding := totalEntryLen - (62 + len(e.Path) + 1 /* this is the \x00 byte */)
		reader.Discard(padding)
		entries = append(entries, e)
	}

	i.index.Entries = entries
	return i.index, nil
}

func readNBytes(r *bufio.Reader, n int) ([]byte, error) {
	if n == 0 || r == nil {
		return nil, nil
	}
	peeked, err := r.Peek(n)
	if err != nil {
		return nil, err
	}
	_, err = r.Discard(n)
	if err != nil {
		return nil, err
	}
	return peeked, nil
}
