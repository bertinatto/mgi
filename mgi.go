package mgi

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type MGIService struct {
	root  string
	obj   *ObjectService
	index *IndexService
}

func NewMGIService(root string, obj *ObjectService, index *IndexService) *MGIService {
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

		blob := &Blob{Data: fileData}
		hash, err := m.obj.StoreObject(blob)
		if err != nil {
			return err
		}

		err = m.index.Add(f, hash)
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

	parent, err := m.currentHead()
	if err != nil {
		return err
	}

	author := os.Getenv("GIT_AUTHOR")
	if author == "" {
		author = os.Getenv("USER")
	}

	authorEmail := os.Getenv("GIT_EMAIL")
	if authorEmail == "" {
		authorEmail = os.Getenv("USER") + "@" + os.Getenv("HOSTNAME")
	}

	c := &Commit{
		Parent:      parent,
		Tree:        tree,
		Author:      author,
		AuthorEmail: authorEmail,
		AuthorTime:  time.Now(),
		Message:     msg,
	}

	hash, err := m.obj.StoreObject(c)
	if err != nil {
		return err
	}

	// Update the tip
	pathMaster := filepath.Join(m.root, "refs", "heads", "master")
	fd, err := os.OpenFile(pathMaster, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer fd.Close()

	fd.WriteString(hash.String())
	fd.WriteString("\n")

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
	return string(bytes.TrimSpace(contents)), nil
}

func (m *MGIService) writeTree() (string, error) {
	index, err := m.index.Read()
	if err != nil {
		return "", err
	}
	hash, err := m.writeSubTree(".", index.Entries)
	if err != nil {
		return "", err
	}
	return hash.String(), nil
}

// writeSubTree writes a tree object for the given path. It may be called recursively.
// The subTree parameter must end with a slash ("/") or it can be an emtpy string (to represent the current directory
func (m *MGIService) writeSubTree(subTree string, entries []*IndexEntry) (*Hash, error) {
	if subTree == "." {
		subTree = ""
	}
	if len(subTree) > 0 && !strings.HasSuffix(subTree, "/") {
		subTree += "/"
	}

	// First of all, we are going to figure out the entries for our tree object
	children := make(map[string]*TreeEntry)
	for _, indexEntry := range entries {
		entryDir, entryFile := filepath.Split(indexEntry.Path)
		if entryDir == subTree {
			// The entry is both a file and a direct child of the subTree, so add it to our tree object listing
			e := &TreeEntry{
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
			hash, err := m.writeSubTree(nextDirPath, entries)
			if err != nil {
				return nil, err
			}

			e := &TreeEntry{
				mode: 040000,
				path: directChild,
			}
			copy(e.sha1[:], hash.Bytes())
			children[e.path] = e
		}
	}

	// Now the we know the entries, write the tree object
	if len(children) > 0 {
		lines := make([]*TreeEntry, 0, len(children))
		for _, v := range children {
			lines = append(lines, v)
		}

		// Entries should be sorted in acending order
		sort.Slice(lines, func(i, j int) bool {
			return lines[i].path < lines[j].path
		})

		t := &Tree{Entries: lines}
		hash, err := m.obj.StoreObject(t)
		if err != nil {
			return nil, err
		}
		return hash, nil
	}

	return nil, fmt.Errorf("failed to write a tree")
}

func (m *MGIService) Diff() (string, error) {
	panic("Implement me")
}

func (m *MGIService) Show() (string, error) {
	panic("Implement me")
}

func (m *MGIService) Status() (string, error) {
	panic("Implement me")
}

func (m *MGIService) Pull(remote string) error {
	panic("Implement me")
}

func (m *MGIService) Push(remote string) error {
	panic("Implement me")
}
