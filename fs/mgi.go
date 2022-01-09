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
type treeEntry struct {
	mode uint32
	path string
	sha1 [20]byte
}

// writeSubTree writes a tree object for the given path. It may be called recursively.
// The subTree parameter must end with a slash ("/") or it can be an emtpy string (to represent the current directory
func (m *MGIService) writeSubTree(subTree string, entries []*mgi.IndexEntry) (string, []byte, error) {
	if subTree == "." {
		subTree = ""
	}
	if len(subTree) > 0 && !strings.HasSuffix(subTree, "/") {
		subTree += "/"
	}

	// First of all, we are going to figure out the entries for our tree object
	children := make(map[string]*treeEntry)
	for _, indexEntry := range entries {
		entryDir, entryFile := filepath.Split(indexEntry.Path)
		if entryDir == subTree {
			// The entry is both a file and a direct child of the subTree, so add it to our tree object listing
			e := &treeEntry{
				mode: indexEntry.Mode,
				// path: filepath.Base(indexEntry.Path),
				path: entryFile,
				sha1: indexEntry.Sha1,
			}
			children[e.path] = e

		} else if strings.HasPrefix(entryDir, subTree) {
			// The entry is somewhere under subTree (not necessarily a direct child, though)
			relEntry := strings.TrimPrefix(entryDir, subTree) // a/b/c, a/ -> b/c
			directChild := strings.Split(relEntry, "/")[0]

			if _, ok := children[directChild]; ok {
				continue
			}

			// Before we can create the tree object for subTree, we need to create an object for this sub-directory
			nextDirPath := filepath.Join(subTree, directChild) + string(os.PathSeparator)
			_, sumBytes, err := m.writeSubTree(nextDirPath, entries)
			if err != nil {
				return "", nil, err
			}

			e := &treeEntry{
				mode: 040000,
				path: directChild,
			}
			copy(e.sha1[:], sumBytes)
			children[e.path] = e
		}
	}

	// Now the we know the entries, write the tree object
	if len(children) > 0 {
		lines := make([]*treeEntry, 0, len(children))
		for _, v := range children {
			lines = append(lines, v)
		}

		// Entries should be sorted in acending order
		sort.Slice(lines, func(i, j int) bool {
			return lines[i].path < lines[j].path
		})

		b := new(bytes.Buffer)
		for _, e := range lines {
			b.WriteString(fmt.Sprintf("%o %s\x00%s", e.mode, e.path, e.sha1))
		}

		sum, sumBytes, err := m.obj.Store(mgi.TreeType, b.Bytes())
		if err != nil {
			return "", nil, err
		}
		return sum, sumBytes[:], nil
	}

	return "", nil, fmt.Errorf("failed to write a tree")
}
