package fs

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bertinatto/mgi"
)

type MGIService struct {
	root  string
	obj   mgi.ObjectService
	index mgi.IndexService
}

func NewMGIService(root string, obj mgi.ObjectService, index mgi.IndexService) *MGIService {
	return &MGIService{
		root:  root,
		obj:   obj,
		index: index,
	}
}

func (m *MGIService) Add(files []string) error {
	_, err := m.index.Read()
	if err != nil {
		return fmt.Errorf("error reading index file: %v", err)
	}

	for _, f := range files {
		fileData, err := ioutil.ReadFile(f)
		if err != nil {
			// TODO: make this atomic instead
			return err
		}

		_, sha1Bytes, err := m.obj.Store(mgi.BlobType, fileData)
		if err != nil {
			return err
		}

		err = m.index.Add(f, sha1Bytes)
		if err != nil {
			return err
		}

	}
	return m.index.Store()
}

func (m *MGIService) Commit(msg string) error {
	tree, err := m.writeTree()
	if err != nil {
		return err
	}

	now := time.Now()
	_, offset := now.Zone()

	// Find out the author time
	var aTime strings.Builder
	var sign string
	if offset > 0 {
		sign = "+"
	} else {
		sign = "-"
	}
	fo := int64(math.Abs(float64(offset)))
	timestamp := int64(math.Abs(float64(now.Unix())))
	aTime.WriteString(fmt.Sprintf("%d %s%02d%02d", timestamp, sign, fo/3600, (fo/60)%60))
	authorTime := aTime.String()

	b := new(bytes.Buffer)
	b.WriteString("tree " + tree)
	b.WriteString("\n")

	parent, err := m.currentHead()
	if err != nil {
		return err
	}

	if parent != "" {
		b.WriteString("parent " + parent)
		b.WriteString("\n")
	}

	b.WriteString(fmt.Sprintf("author %s %s", "Fabio <fabio@fabio>", authorTime))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("committer %s %s", "Fabio <fabio@fabio>", authorTime))
	b.WriteString("\n")
	b.WriteString("\n")
	b.WriteString(msg)
	b.WriteString("\n")

	sha1, _, err := m.obj.Store(mgi.CommitType, b.Bytes())
	if err != nil {
		return err
	}

	pathMaster := filepath.Join(m.root, "refs", "heads", "master")
	fd, err := os.OpenFile(pathMaster, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer fd.Close()

	fd.WriteString(sha1)
	fd.WriteString("\n")

	return nil
}

func (m *MGIService) Diff() (string, error) {
	return "", nil
}

func (m *MGIService) Show() (string, error) {
	return "", nil
}

func (m *MGIService) Status() (string, error) {
	return "", nil
}

func (m *MGIService) Pull(remote string) error {
	return nil
}

func (m *MGIService) Push(remote string) error {
	return nil
}

func (m *MGIService) currentHead() (string, error) {
	pathMaster := filepath.Join(m.root, "refs", "heads", "master")
	contents, err := ioutil.ReadFile(pathMaster)
	if os.IsNotExist(err) {
		return "", nil
	}

	if err != nil {
		return "", err
	}

	sha1 := bytes.TrimSpace(contents)
	return string(sha1), nil
}

func (m *MGIService) writeTree() (string, error) {
	index, err := m.index.Read()
	if err != nil {
		return "", err
	}

	sum, _, err := m.writeSubTree(".", index.Entries)
	return sum, err
}

type TreeEntry struct {
	Mode uint32
	Path string
	Sha1 [20]byte
}

func (m *MGIService) writeSubTree(subTree string, entries []*mgi.IndexEntry) (string, []byte, error) {
	if subTree == "." {
		subTree = ""
	}

	// b := new(bytes.Buffer)
	// var lines []*TreeEntry
	kv := make(map[string]*TreeEntry)
	for _, entry := range entries {
		dir, _ := filepath.Split(entry.Path)
		if dir == subTree {
			// The entry is a direct child of the subTree, so add it to our object
			// line := fmt.Sprintf("%o %s\x00%s", entry.Mode, filepath.Base(entry.Path), string(entry.Sha1[:]))
			// b.WriteString(line)
			e := &TreeEntry{
				Mode: entry.Mode,
				Path: filepath.Base(entry.Path),
				Sha1: entry.Sha1,
			}
			// lines = append(lines, e)
			kv[e.Path] = e
		} else {
			parentDir := filepath.Dir(strings.TrimSuffix(dir, "/"))
			if parentDir == "." {
				parentDir = ""
			}
			if subTree == parentDir {
				// This entry is located at: subTree/X/entry. We need to find out
				// the sum of X before we can write the tree object for subTree
				_, sumBytes, err := m.writeSubTree(dir, entries)
				if err != nil {
					return "", nil, err
				}
				// b.WriteString(fmt.Sprintf("%o %s\x00%s", 040000, filepath.Base(dir), sumBytes))
				e := &TreeEntry{
					Mode: 040000,
					Path: filepath.Base(dir),
				}
				copy(e.Sha1[:], sumBytes)
				// lines = append(lines, e)
				kv[e.Path] = e
			}
		}
	}

	// if len(lines) > 0 {
	if len(kv) > 0 {
		lines := make([]*TreeEntry, 0, len(kv))
		for _, v := range kv {
			lines = append(lines, v)
		}

		// Entries should be sorted in acending order
		sort.Slice(lines, func(i, j int) bool {
			return lines[i].Path < lines[j].Path
		})

		b := new(bytes.Buffer)
		for _, e := range lines {
			b.WriteString(fmt.Sprintf("%o %s\x00%s", e.Mode, e.Path, e.Sha1))
		}

		sum, sumBytes, err := m.obj.Store(mgi.TreeType, b.Bytes())
		if err != nil {
			return "", nil, err
		}
		return sum, sumBytes[:], nil
	}

	return "", nil, fmt.Errorf("failed to write a tree")
}
