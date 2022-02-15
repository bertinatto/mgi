package main

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	"github.com/bertinatto/mgi"
)

func main() {
	const rootLocation = ".git"

	// Commands
	initCmd := flag.NewFlagSet("init", flag.ExitOnError)
	addCmd := flag.NewFlagSet("add", flag.ExitOnError)
	commitCmd := flag.NewFlagSet("commit", flag.ExitOnError)
	statusCmd := flag.NewFlagSet("status", flag.ExitOnError)
	diffCmd := flag.NewFlagSet("diff", flag.ExitOnError)

	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Available subcommands: init, add, commit, status, diff")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "init":
		initCmd.Parse(nil)
		err := doInit(rootLocation)
		if err != nil {
			log.Fatalf("Failed to initialize directories: %v", err)
		}
	case "add":
		addCmd.Parse(os.Args[2:])
		indexService := mgi.NewIndexService(rootLocation)
		obj := mgi.NewObjectService(rootLocation)
		mgi := mgi.NewMGIService(rootLocation, obj, indexService)
		err := mgi.Add(addCmd.Args())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error adding files: %v", err)
			os.Exit(1)
		}
	case "commit":
		commitCmd.Parse(os.Args[2:])
		opts := commitCmd.Args()
		if len(opts) < 1 {
			fmt.Fprintf(os.Stderr, "commit command needs a message")
			os.Exit(1)
		}
		indexService := mgi.NewIndexService(rootLocation)
		obj := mgi.NewObjectService(rootLocation)
		mgi := mgi.NewMGIService(rootLocation, obj, indexService)
		err := mgi.Commit(opts[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error committing files: %v", err)
			os.Exit(1)
		}
	case "status":
		statusCmd.Parse(os.Args[2:])
		opts := statusCmd.Args()
		if len(opts) > 0 {
			fmt.Fprintf(os.Stderr, "status command does not have arguments")
			os.Exit(1)
		}

		indexService := mgi.NewIndexService(rootLocation)
		obj := mgi.NewObjectService(rootLocation)
		mgi := mgi.NewMGIService(rootLocation, obj, indexService)

		untracked, modified, err := mgi.Status()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error checking status: %v", err)
			os.Exit(1)
		}

		if len(untracked) > 0 {
			fmt.Printf("Untracked files:\n")
			for i := range untracked {
				fmt.Printf("\t%s\n", untracked[i])
			}
		}

		if len(modified) > 0 {
			fmt.Printf("Modified files:\n")
			for i := range modified {
				fmt.Printf("\t%s\n", modified[i])
			}
		}
	case "diff":
		diffCmd.Parse(os.Args[2:])
		opts := diffCmd.Args()
		if len(opts) > 0 {
			fmt.Fprintf(os.Stderr, "diff command does not have arguments")
			os.Exit(1)
		}

		indexService := mgi.NewIndexService(rootLocation)
		obj := mgi.NewObjectService(rootLocation)
		mgi := mgi.NewMGIService(rootLocation, obj, indexService)

		diffs, err := mgi.Diff()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error checking diff: %v", err)
			os.Exit(1)
		}

		for i := range diffs {
			fmt.Printf("%s\n", diffs[i])
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown command")
		os.Exit(1)
	}
}

func doInit(root string) error {
	dirs := []string{
		"objects",
		"refs",
		"refs/heads",
	}
	for _, d := range dirs {
		path := filepath.Join(root, d)
		err := os.MkdirAll(path, 0755)
		if err != nil {
			return fmt.Errorf("failed to create directory %q: %v", path, err)
		}
	}

	headPath := filepath.Join(root, "HEAD")
	_, err := os.Stat(headPath)
	if errors.Is(err, os.ErrNotExist) {
		err := ioutil.WriteFile(headPath, []byte("ref: refs/heads/master"), 0755)
		if err != nil {
			return fmt.Errorf("failed to write the master file: %v", err)
		}
	} else if err != nil {
		return fmt.Errorf("failed to read file %q: %v", headPath, err)
	}

	return nil
}
