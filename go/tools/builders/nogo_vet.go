// Copyright 2018 The Bazel Authors. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// vet runs the 'go vet tool' command with the given vet configuration file and
// returns the emitted findings. It returns an error only if vet emits no
// findings and at least one error message.
func runVet(vetTool, vcfg string) (string, error) {
	args := append(vetFlags, vcfg)
	cmd := exec.Command(vetTool, args...)
	out, err := cmd.CombinedOutput()
	// Note: 'go tool vet' emits a non-zero return code both when vet encounters
	// an internal error and when vet finds legitimate issues.
	if err == nil {
		return "", nil
	}
	vetFindings, vetErrors := []string{}, []string{}
	errMsgs := splitOutput(string(out))
	for _, m := range errMsgs {
		if !vetErrorMsgPattern.MatchString(m) {
			vetErrors = append(vetErrors, m)
			continue
		}
		vetFindings = append(vetFindings, m)
	}
	if len(vetFindings) == 0 && len(vetErrors) != 0 {
		return "", errors.New(strings.Join(vetErrors, "\n"))
	}
	return strings.Join(vetFindings, "\n"), nil
}

var vetFlags = []string{
	// NOTE: Keep in sync with github.com/golang/go/src/cmd/go/internal/test/test.go
	"-atomic",
	"-bool",
	"-buildtags",
	"-nilfunc",
	"-printf",
}

// vetErrorMsgPattern matches an error message emitted by vet.
//
// This regexp should be strict enough to exclude internal errors (prefixed with
// "vet") and type-check errors (which additionally include a column number).
//
// NOTE: this might break if the formating of vet error messages changes.
var vetErrorMsgPattern = regexp.MustCompile(`^([^:]+?):([0-9]+): (.*)`)

// splitOutput was adapted from go/test/run.go.
func splitOutput(out string) []string {
	// gc error messages continue onto additional lines with leading tabs.
	// Split the output at the beginning of each line that doesn't begin with a tab.
	// <autogenerated> lines are impossible to match so those are filtered out.
	var res []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSuffix(line, "\r") // normalize Windows output
		if strings.HasPrefix(line, "\t") {
			res[len(res)-1] += "\n" + line
		} else if strings.HasPrefix(line, "go tool") || strings.HasPrefix(line, "#") {
			continue
		} else if strings.TrimSpace(line) != "" {
			res = append(res, line)
		}
	}
	return res
}

// buildVetcfgFile creates a vet.cfg file and returns its file path. It is the
// caller's responsibility to remove this file when it is no longer needed.
func buildVetcfgFile(packageFile, importMap map[string]string, stdImports, files []string) (string, error) {
	for path := range packageFile {
		if _, ok := importMap[path]; !ok {
			// vet expects every import path to be in the import map, even if the
			// mapping is redundant.
			importMap[path] = path
		}
	}
	vcfg := &vetConfig{
		Compiler:                  "gc", // gccgo is currently not supported
		GoFiles:                   files,
		ImportMap:                 importMap,
		PackageFile:               packageFile,
		Standard:                  make(map[string]bool),
		SucceedOnTypecheckFailure: false,
	}
	for _, imp := range stdImports {
		vcfg.Standard[imp] = true
	}

	js, err := json.MarshalIndent(vcfg, "", "\t")
	if err != nil {
		return "", fmt.Errorf("internal error marshaling vet config: %v", err)
	}
	js = append(js, '\n')
	vcfgFile := filepath.Join(os.TempDir(), "vet.cfg")
	if err := ioutil.WriteFile(vcfgFile, js, 0666); err != nil {
		return "", err
	}
	return vcfgFile, nil
}

// vetConfig is the configuration passed to vet describing a single package.
// NOTE: Keep in sync with github.com/golang/go/internal/work/exec.go
type vetConfig struct {
	Compiler   string   // compiler name (gc, gccgo)
	Dir        string   // directory containing package
	ImportPath string   // canonical import path ("package path")
	GoFiles    []string // absolute paths to package source files

	ImportMap   map[string]string // map import path in source code to package path
	PackageFile map[string]string // map package path to .a file with export data
	Standard    map[string]bool   // map package path to whether it's in the standard library
	PackageVetx map[string]string // map package path to vetx data from earlier vet run
	VetxOnly    bool              // only compute vetx data; don't report detected problems
	VetxOutput  string            // write vetx data to this output file

	SucceedOnTypecheckFailure bool // awful hack; see #18395 and below
}
