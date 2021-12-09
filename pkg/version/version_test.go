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

package version

import (
	"testing"
)

func TestParse(t *testing.T) {
	type versionStringTest struct {
		input string
		want  string
	}
	// versionString syntax:
	//   <release tag>-<commits since release tag>-<commit hash>-<branch name>
	tests := []versionStringTest{
		{input: "0.6.6-0-g12345678-master", want: "v0.6.6"},
		{input: "0.6.6-0-g12345678-main", want: "v0.6.6"},
		{input: "0.6.6-0-g12345678-HEAD", want: "v0.6.6"},
		{input: "0.6.6-0-g87654321-custom", want: "v0.6.6-custom"},
		{input: "0.6.6-2-gabcdef01-master", want: "v0.6.6-master (abcdef01, +2)"},
		{input: "0.6.6-1-g123456ab-custom", want: "v0.6.6-custom (123456ab, +1)"},
		{input: "0.6.6-rc1-0-g12345678-master", want: "v0.6.6-rc1"},
		{input: "0.6.6-internal-rc1-0-g12345678-master", want: "v0.6.6-internal-rc1"},
		{input: "0.6.6-internal-rc1-0-g12345678-main", want: "v0.6.6-internal-rc1"},
		{input: "0.6.6-internal-rc1-0-g12345678-HEAD", want: "v0.6.6-internal-rc1"},
		{input: "0.6.6-rc1-g12345678-master", want: "v0.0.0-unofficial"}, // unparseable: no commits present
		{input: "", want: "v0.0.0-unofficial"},
		{input: "0.6.6-rc1-15-g12345678-want-more-branch", want: "v0.6.6-rc1-want-more-branch (12345678, +15)"}, // branch name with hypens should be captured.
		{input: "v0.6.6-rc1-15-g12345678-want-more-branch", want: "v0.6.6-rc1-want-more-branch (12345678, +15)"},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			build = test.input
			if have := Parse(); test.want != have {
				t.Errorf("want: %s, have: %s", test.want, have)
			}
		})
	}
}
