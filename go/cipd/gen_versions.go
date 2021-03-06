// +build ignore

package main

/*
	Generate asset_versions_gen.go.
*/

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"runtime"
	"sort"
	"strings"

	"go.skia.org/infra/go/exec"
	"go.skia.org/infra/go/sklog"
)

const (
	TARGET_FILE = "asset_versions_gen.go"
	TMPL        = `// Code generated by "go run gen_versions.go"; DO NOT EDIT

package cipd

var PKG_VERSIONS_FROM_ASSETS = map[string]string{
%s}
`
)

func main() {
	_, filename, _, _ := runtime.Caller(0)
	pkgDir := path.Dir(filename)
	rootDir := path.Join(pkgDir, "..", "..")

	// List the assets.
	assetsDir := path.Join(rootDir, "infra", "bots", "assets")
	entries, err := ioutil.ReadDir(assetsDir)
	if err != nil {
		sklog.Fatal(err)
	}
	assets := make(map[string]string, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			contents, err := ioutil.ReadFile(path.Join(assetsDir, e.Name(), "VERSION"))
			if err == nil {
				assets[e.Name()] = strings.TrimSpace(string(contents))
			} else if !os.IsNotExist(err) {
				sklog.Fatal(err)
			}
		}
	}

	assetLines := make([]string, 0, len(assets))
	for name, version := range assets {
		line := fmt.Sprintf("\t\"%s\": \"%s\",\n", name, version)
		assetLines = append(assetLines, line)
	}
	sort.Strings(assetLines)
	assetsStr := strings.Join(assetLines, "")
	fileContents := []byte(fmt.Sprintf(TMPL, assetsStr))
	targetFile := path.Join(pkgDir, TARGET_FILE)
	if err := ioutil.WriteFile(targetFile, fileContents, os.ModePerm); err != nil {
		sklog.Fatal(err)
	}
	if _, err := exec.RunCwd(".", "gofmt", "-s", "-w", targetFile); err != nil {
		sklog.Fatal(err)
	}
}
