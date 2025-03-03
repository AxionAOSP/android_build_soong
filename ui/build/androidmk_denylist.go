// Copyright 2024 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package build

import (
	"strings"
)

var androidmk_denylist []string = []string{
	"bionic/",
	"chained_build_config/",
	"cts/",
	"dalvik/",
	"developers/",
	"development/",
	"device/common/",
	"device/google_car/",
	"device/sample/",
	"frameworks/",
	"hardware/libhardware/",
	"hardware/libhardware_legacy/",
	"hardware/ril/",
	// Do not block other directories in kernel/, see b/319658303.
	"kernel/configs/",
	"kernel/prebuilts/",
	"kernel/tests/",
	"libcore/",
	"libnativehelper/",
	"packages/",
	"pdk/",
	"platform_testing/",
	"prebuilts/",
	"sdk/",
	"system/",
	"test/",
	"trusty/",
	// Add back toolchain/ once defensive Android.mk files are removed
	//"toolchain/",
	"vendor/google_contexthub/",
	"vendor/google_data/",
	"vendor/google_elmyra/",
	"vendor/google_mhl/",
	"vendor/google_pdk/",
	"vendor/google_testing/",
	"vendor/partner_testing/",
	"vendor/partner_tools/",
	"vendor/pdk/",
}

func blockAndroidMks(ctx Context, androidMks []string) {
	for _, mkFile := range androidMks {
		for _, d := range androidmk_denylist {
			if strings.HasPrefix(mkFile, d) {
				ctx.Fatalf("Found blocked Android.mk file: %s. "+
					"Please see androidmk_denylist.go for the blocked directories and contact build system team if the file should not be blocked.", mkFile)
			}
		}
	}
}

var external_androidmks []string = []string{
	// The Android.mk files in these directories are for NDK build system.
	"external/fmtlib/",
	"external/google-breakpad/",
	"external/googletest/",
	"external/libaom/",
	"external/libusb/",
	"external/libvpx/",
	"external/libwebm/",
	"external/libwebsockets/",
	"external/vulkan-validation-layers/",
	"external/walt/",
	"external/webp/",
	// These directories hold the published Android SDK, used in Unbundled Gradle builds.
	"prebuilts/fullsdk-darwin",
	"prebuilts/fullsdk-linux",
}

var art_androidmks = []string{
	//"art/",
}

func ignoreSomeAndroidMks(androidMks []string) (filtered []string) {
	ignore_androidmks := make([]string, 0, len(external_androidmks)+len(art_androidmks))
	ignore_androidmks = append(ignore_androidmks, external_androidmks...)
	ignore_androidmks = append(ignore_androidmks, art_androidmks...)

	shouldKeep := func(androidmk string) bool {
		for _, prefix := range ignore_androidmks {
			if strings.HasPrefix(androidmk, prefix) {
				return false
			}
		}
		return true
	}

	for _, l := range androidMks {
		if shouldKeep(l) {
			filtered = append(filtered, l)
		}
	}
	return
}
