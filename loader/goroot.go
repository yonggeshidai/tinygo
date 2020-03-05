package loader

// This file constructs a new temporary GOROOT directory by merging both the
// standard Go GOROOT and the GOROOT from TinyGo using symlinks.

import (
	"errors"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"

	"github.com/tinygo-org/tinygo/compileopts"
	"github.com/tinygo-org/tinygo/goenv"
)

// CreateTemporaryGoroot creates a new temporary GOROOT by merging both the
// standard GOROOT and the GOROOT from TinyGo using lots of symbolic links. This
// new directory should be removed after use.
func CreateTemporaryGoroot(config *compileopts.Config) (tmpgoroot string, err error) {
	goroot := goenv.Get("GOROOT")
	if goroot == "" {
		return "", errors.New("could not determine GOROOT")
	}
	tinygoroot := goenv.Get("TINYGOROOT")
	if tinygoroot == "" {
		return "", errors.New("could not determine TINYGOROOT")
	}
	tmpgoroot, err = ioutil.TempDir("", "tinygo-goroot")
	if err != nil {
		return
	}
	for _, name := range []string{"bin", "lib", "pkg"} {
		err = os.Symlink(filepath.Join(goroot, name), filepath.Join(tmpgoroot, name))
		if err != nil {
			return
		}
	}
	err = mergeDirectory(goroot, tinygoroot, tmpgoroot, "", pathsToOverride(config.BuildTags()))
	if err != nil {
		return
	}
	return
}

// mergeDirectory merges two roots recursively. The tmpgoroot is the directory
// that will be created by this call by either symlinking the directory from
// goroot or tinygoroot, or by creating the directory and merging the contents.
func mergeDirectory(goroot, tinygoroot, tmpgoroot, importPath string, overrides map[string]bool) error {
	if mergeSubdirs, ok := overrides[importPath+"/"]; ok {
		if !mergeSubdirs {
			// This directory and all subdirectories should come from the TinyGo
			// root, so simply make a symlink.
			newname := filepath.Join(tmpgoroot, "src", importPath)
			oldname := filepath.Join(tinygoroot, "src", importPath)
			return os.Symlink(oldname, newname)
		}

		// Merge subdirectories. Start by making the directory to merge.
		err := os.Mkdir(filepath.Join(tmpgoroot, "src", importPath), 0777)
		if err != nil {
			return err
		}

		// Symlink all files from TinyGo, and symlink directories from TinyGo
		// that need to be overridden.
		tinygoEntries, err := ioutil.ReadDir(filepath.Join(tinygoroot, "src", importPath))
		if err != nil {
			return err
		}
		for _, e := range tinygoEntries {
			if e.IsDir() {
				// A directory, so merge this thing.
				err := mergeDirectory(goroot, tinygoroot, tmpgoroot, path.Join(importPath, e.Name()), overrides)
				if err != nil {
					return err
				}
			} else {
				// A file, so symlink this.
				newname := filepath.Join(tmpgoroot, "src", importPath, e.Name())
				oldname := filepath.Join(tinygoroot, "src", importPath, e.Name())
				err := os.Symlink(oldname, newname)
				if err != nil {
					return err
				}
			}
		}

		// Symlink all directories from $GOROOT that are not part of the TinyGo
		// overrides.
		gorootEntries, err := ioutil.ReadDir(filepath.Join(goroot, "src", importPath))
		if err != nil {
			return err
		}
		for _, e := range gorootEntries {
			if !e.IsDir() {
				// Don't merge in files from Go. Otherwise we'd end up with a
				// weird syscall package with files from both roots.
				continue
			}
			if _, ok := overrides[path.Join(importPath, e.Name())+"/"]; ok {
				// Already included above, so don't bother trying to create this
				// symlink.
				continue
			}
			newname := filepath.Join(tmpgoroot, "src", importPath, e.Name())
			oldname := filepath.Join(goroot, "src", importPath, e.Name())
			err := os.Symlink(oldname, newname)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// The boolean indicates whether to merge the subdirs. True means merge, false
// means use the TinyGo version.
func pathsToOverride(buildTags []string) map[string]bool {
	paths := map[string]bool{
		"/":                     true,
		"device/":               false,
		"examples/":             false,
		"internal/":             true,
		"internal/reflectlite/": false,
		"machine/":              false,
		"os/":                   true,
		"reflect/":              false,
		"runtime/":              false,
		"sync/":                 true,
		"testing/":              false,
	}
	for _, tag := range buildTags {
		if tag == "baremetal" || tag == "darwin" {
			paths["syscall/"] = true // include syscall/js
		}
	}
	return paths
}
