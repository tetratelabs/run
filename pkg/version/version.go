// Copyright (c) Tetrate, Inc 2021.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package version can be used to implement embedding versioning details from
// git branches and tags into the binary importing this package.
package version

import (
	"fmt"
	"strconv"
	"strings"
)

// build is to be populated at build time using -ldflags -X.
//
// Example:
//   VERSION_PATH    := github.com/tetratelabs/run/pkg/version
//   VERSION_STRING  := $(shell git describe --tags --long)
//   GIT_BRANCH_NAME := $(shell git rev-parse --abbrev-ref HEAD)
//   GO_LINK_VERSION := -X ${VERSION_PATH}.build=${VERSION_STRING}-${GIT_BRANCH_NAME}
//   go build -ldflags '${GO_LINK_VERSION}'
var build string

// mainBranches is a list of (sorted) main branches/revisions.
var mainBranches = []string{"HEAD", "main", "master"}

// Show the service's version information
func Show(serviceName string) {
	fmt.Println(serviceName + " " + Parse())
}

// Parse returns the parsed service's version information. (from raw git label)
func Parse() string {
	return parseGit(build).String()
}

// Git contains the version information extracted from a Git SHA.
type Git struct {
	ClosestTag   string
	CommitsAhead int
	Sha          string
	Branch       string
}

func (g Git) String() string {
	switch {
	case g == Git{}:
		// unofficial version built without using the make tooling
		return "v0.0.0-unofficial"
	case g.CommitsAhead != 0:
		// built from a non release commit point
		// In the version string, the commit tag is prefixed with "-g" (which stands for "git").
		// When printing the version string, remove that prefix to just show the real commit hash.
		return fmt.Sprintf("%s-%s (%s, +%d)", g.ClosestTag, g.Branch, g.Sha, g.CommitsAhead)
	case !isMainBranch(g.Branch):
		// specific branch release build
		return fmt.Sprintf("%s-%s", g.ClosestTag, g.Branch)
	default:
		return g.ClosestTag
	}
}

// parseGit the given version string into a version object. The input version string
// is in the format:
//    <release tag>-<commits since release tag>-g<commit hash>-<branch name>
func parseGit(v string) Git {
	parts := strings.Split(v, "-")
	l := len(parts)
	if l < 4 {
		return Git{}
	}

	// The git tag could contain '-' characters, so we start parting the version string
	// from the last parts, and concatenate the remaining ones at the beginning to reconstruct
	// the original tag if it had '-' characters.

	commits, err := strconv.Atoi(parts[l-3])
	if err != nil { // extra safety but should never happen
		return Git{}
	}

	// prefix v on semantic versioning tags omitting it
	// Go module tags should include the 'v'
	if strings.ToLower(parts[0])[0] != 'v' {
		parts[0] = "v" + parts[0]
	}

	return Git{
		ClosestTag:   strings.Join(parts[:l-3], "-"),
		CommitsAhead: commits,
		Sha:          parts[l-2][1:], // remove the 'g' prefix
		Branch:       parts[l-1],
	}
}

// isMainBranch returns true if the given branch name is a main branch.
func isMainBranch(branch string) bool {
	for _, b := range mainBranches {
		if b == branch {
			return true
		}
	}
	return false
}
