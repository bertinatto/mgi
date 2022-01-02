package fs

import (
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

	"github.com/bertinatto/mgi"
)

type IndexService struct {
	path  string
	index *mgi.Index
}

func NewIndexService(root string) *IndexService {
	return &IndexService{
		path: filepath.Join(root, "index"),
		index: &mgi.Index{
			Signature: "DIRC",
			Version:   "2",
		},
	}
}

// todo: before calling this, the user needs to o.Store(mgi.BlobType, bytes)
func (i *IndexService) Add(path string, sum1 [20]byte) error {
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

	entry := &mgi.IndexEntry{
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
		Sha1:          sum1,
		Flags:         uint16(len(path)),
		Path:          path,
	}

	i.index.Entries = append(i.index.Entries, entry)
	i.index.EntryCount = len(i.index.Entries)
	return nil
}

func (i *IndexService) Store() error {
	mb := new(bytes.Buffer)

	var signature [4]byte
	if len(i.index.Signature) != 4 {
		return fmt.Errorf("signature must be 4 bytes long")
	}
	copy(signature[:], []byte(i.index.Signature))
	binary.Write(mb, binary.BigEndian, signature)

	version, err := strconv.Atoi(i.index.Version)
	if err != nil {
		return err
	}
	binary.Write(mb, binary.BigEndian, uint32(version))
	binary.Write(mb, binary.BigEndian, uint32(i.index.EntryCount))

	// Index entries should be sorted
	sort.Slice(i.index.Entries, func(x, y int) bool {
		return i.index.Entries[x].Path < i.index.Entries[y].Path
	})

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
		binary.Write(b, binary.BigEndian, v.Sha1)
		binary.Write(b, binary.BigEndian, v.Flags)
		b.WriteString(v.Path)
		b.WriteString("\x00")

		length := ((62 + len(v.Path) + 8) / 8) * 8
		for b.Len() < length {
			b.WriteString("\x00")
		}
		mb.Write(b.Bytes())
	}

	sum := sha1.Sum(mb.Bytes())
	binary.Write(mb, binary.BigEndian, sum)

	fd, err := os.OpenFile(i.path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer fd.Close()

	_, err = fd.Write(mb.Bytes())
	return err
}

func (i *IndexService) Read() (*mgi.Index, error) {
	data, err := ioutil.ReadFile(i.path)
	if errors.Is(err, os.ErrNotExist) {
		return &mgi.Index{
			Signature:  "DIRC",
			Version:    "2",
			EntryCount: 0,
			Entries:    nil,
			Hash:       "", // This will be set once the file is writtten to disk
		}, nil
	}
	if err != nil {
		return nil, err
	}

	// TODO: validate index file
	i.index.Signature = string(data[:4])
	i.index.Version = fmt.Sprintf("%d", binary.BigEndian.Uint32(data[4:8]))
	i.index.EntryCount = int(binary.BigEndian.Uint32(data[8:12]))
	i.index.Hash = fmt.Sprintf("%x", data[len(data)-20:])

	if i.index.Version != "2" {
		return nil, fmt.Errorf("unsupported version %q", i.index.Version)
	}

	d := fmt.Sprintf("%x", sha1.Sum(data[:len(data)-20]))
	if i.index.Hash != d {
		return nil, fmt.Errorf("digests don't match")
	}

	// Read entries
	entries := make([]*mgi.IndexEntry, 0, 10)
	for idx := 12; idx < len(data)-20 && len(entries) < i.index.EntryCount; {
		entry := new(mgi.IndexEntry)
		j := idx
		entry.CTimeSecs = binary.BigEndian.Uint32(data[j : j+4])
		j += 4
		entry.CTimeNanoSecs = binary.BigEndian.Uint32(data[j : j+4])
		j += 4
		entry.MTimeSecs = binary.BigEndian.Uint32(data[j : j+4])
		j += 4
		entry.MTimeNanoSecs = binary.BigEndian.Uint32(data[j : j+4])
		j += 4
		entry.Dev = binary.BigEndian.Uint32(data[j : j+4])
		j += 4
		entry.Ino = binary.BigEndian.Uint32(data[j : j+4])
		j += 4
		entry.Mode = binary.BigEndian.Uint32(data[j : j+4])
		j += 4
		entry.Uid = binary.BigEndian.Uint32(data[j : j+4])
		j += 4
		entry.Gid = binary.BigEndian.Uint32(data[j : j+4])
		j += 4
		entry.FileSize = binary.BigEndian.Uint32(data[j : j+4])
		j += 4

		copy(entry.Sha1[:], data[j:j+20])
		j += 20

		entry.Flags = binary.BigEndian.Uint16(data[j : j+2])
		j += 2

		end := bytes.IndexByte(data[j:], '\x00')
		entry.Path = string(data[j : j+end])
		j += end

		idx += ((62 + len(entry.Path) + 8) / 8) * 8

		entries = append(entries, entry)
	}

	i.index.Entries = entries
	return i.index, nil
}
