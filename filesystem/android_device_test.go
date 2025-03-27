// Copyright 2025 Google Inc. All rights reserved.
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

package filesystem

import (
	"android/soong/android"
	"testing"
)

func TestCrossPartitionOverridesError(t *testing.T) {
	fixture.ExtendWithErrorHandler(
		android.FixtureExpectsOneErrorPattern("vendorlib_overrides_systemlib overrides systemlib, but they belong to separate android_filesystem."),
	).RunTestWithBp(t, `
		android_device {
			name: "my_android_device",
			system_partition_name: "systemimage",
			vendor_partition_name: "vendorimage",
		}

		android_filesystem {
			name: "systemimage",
			deps: ["systemlib"],
			compile_multilib: "both",
		}
		android_filesystem {
			name: "vendorimage",
			partition_type: "vendor",
			deps: ["vendorlib_overrides_systemlib"],
			compile_multilib: "both",
		}

		cc_binary {
			name: "systemlib",
		}

		cc_binary {
			name: "vendorlib_overrides_systemlib",
			vendor: true,
			overrides: ["systemlib"],
		}
	`)
}
