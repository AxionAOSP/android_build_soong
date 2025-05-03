// Copyright 2025 The Android Open Source Project
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

package cipd

import (
	"android/soong/android"
	"slices"
	"testing"
)

func TestCipdPackage(t *testing.T) {
	bp := `
	cipd_package {
		name: "cipd_package1",
		package: "android/prebuilts/package1",
		version: "version1",
		files: [
			"package1_file1",
			"package1_file2",
		],
		resolved_versions_file: "cipd.versions",
	}
	`

	fixture := android.GroupFixturePreparers(
		android.PrepareForTestWithAndroidBuildComponents,
		android.FixtureRegisterWithContext(RegisterCipdComponents),
	)

	export := fixture.RunTestWithBp(t, bp).ModuleForTests(t, "cipd_package1", "").Rule("cipd_export")
	wantInput := "out/soong/.intermediates/cipd_package1/ensure.txt"
	if export.Input.String() != wantInput {
		t.Errorf("export.Input.String() = %v, want %v", export.Input.String(), wantInput)
	}
	if len(export.Inputs) != 0 {
		t.Errorf("len(export.Inputs) = %v, want 0", len(export.Inputs))
	}
	wantRoot := "out/soong/.intermediates/cipd_package1/package"
	wantOutputs := []string{
		wantRoot + "/package1_file1",
		wantRoot + "/package1_file2",
	}
	var gotOutputs []string
	for _, output := range export.Outputs {
		gotOutputs = append(gotOutputs, output.String())
	}
	if !slices.Equal(wantOutputs, gotOutputs) {
		t.Errorf("export.Outputs = %v, want %v", gotOutputs, wantOutputs)
	}
	if export.Output != nil {
		t.Errorf("export.Output = %v, want nil", export.Output)
	}
	if export.Args["root"] != wantRoot {
		t.Errorf("export.Args{\"root\"] = %v, want %v", export.Args["root"], wantRoot)
	}
	if len(export.Args) != 1 {
		t.Errorf("len(export.Args) = %v, want 1", len(export.Args))
	}
}
