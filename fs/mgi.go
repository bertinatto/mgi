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
		f := strings.Trim(f, "./")
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

// TODO: move this to object.go
type TreeEntry struct {
	Mode uint32
	Path string
	Sha1 [20]byte
}

func (m *MGIService) writeSubTree(subTree string, entries []*mgi.IndexEntry) (string, []byte, error) {
	if subTree == "." {
		subTree = ""
	}

	children := make(map[string]*TreeEntry)
	for _, indexEntry := range entries {
		// TODO: replace Split with Dir, so that the trailing / is gone
		entryDir, _ := filepath.Split(indexEntry.Path)
		if entryDir == subTree {
			// The entry is a direct child of the subTree, so add it to our object
			e := &TreeEntry{
				Mode: indexEntry.Mode,
				Path: filepath.Base(indexEntry.Path),
				Sha1: indexEntry.Sha1,
			}
			children[e.Path] = e

		} else {
			// We are only interested in direct childs
			if strings.HasPrefix(entryDir, subTree) {
				// Hey, dir is a child of subTree, but is it a direct child?
				directChild := strings.TrimPrefix(entryDir, subTree) // a/b/c, a/ -> b/c
				dirs := strings.Split(directChild, "/")

				directChild = dirs[0]
				if directChild == "" {
					continue
				}
				if _, ok := children[directChild]; ok {
					continue
				}

				nextDirPath := filepath.Join(subTree, directChild) + "/"
				_, sumBytes, err := m.writeSubTree(nextDirPath, entries)
				if err != nil {
					return "", nil, err
				}

				e := &TreeEntry{
					Mode: 040000,
					Path: directChild,
				}
				copy(e.Sha1[:], sumBytes)
				children[e.Path] = e
			}
		}
	}

	if len(children) > 0 {
		lines := make([]*TreeEntry, 0, len(children))
		for _, v := range children {
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
