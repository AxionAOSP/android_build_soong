#!/usr/bin/env python3
#
# Copyright 2025 The Android Open Source Project
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

import argparse
import os
import struct
import subprocess
import sys
import zipfile

from ninja_determinism_test import Product, get_top

def run_build_target_files_zip(product: Product, soong_only: bool) -> bool:
    """Runs a build and returns if it succeeded or not."""
    soong_only_arg = '--no-soong-only'
    if soong_only:
        soong_only_arg = '--soong-only'

    with open(os.path.join(os.getenv('OUT_DIR', 'out'), 'build.log'), 'wb') as f:
        result = subprocess.run([
            'build/soong/soong_ui.bash',
            '--make-mode',
            'USE_RBE=true',
            'BUILD_DATETIME=1',
            'USE_FIXED_TIMESTAMP_IMG_FILES=true',
            f'TARGET_PRODUCT={product.product}',
            f'TARGET_RELEASE={product.release}',
            f'TARGET_BUILD_VARIANT={product.variant}',
            'target-files-package',
            'dist',
            soong_only_arg,
        ], stdout=f, stderr=subprocess.STDOUT, env=os.environ)
        return result.returncode == 0

# These values are defined in build/soong/zip/zip.go
SHA_256_HEADER_ID = 0x4967
SHA_256_HEADER_SIGNATURE = 0x9514

def get_local_file_sha256_fields(zip_filepath: os.PathLike) -> dict[str, bytes]:
    if not os.path.exists(zip_filepath):
        print(f"Error: File not found at {zip_filepath}", file=sys.stderr)
        return None

    sha256_checksums: dict[str, bytes] = {}

    with zipfile.ZipFile(zip_filepath, 'r') as zip_ref:
        infolist = zip_ref.infolist()

        for member_info in infolist:
            # Skip if the entry is a directory or does not contain the sha256 value, which
            # is included in the extra field.
            if member_info.is_dir() or len(member_info.extra) == 0:
                continue

            local_extra_data = member_info.extra

            i = 0
            found_sha_in_file = None
            while i + 4 <= len(local_extra_data): # Need at least 4 (header ID + data size)
                block_header_id, block_data_size = struct.unpack('<HH', local_extra_data[i:i+4])

                current_block_end = i + 4 + block_data_size

                # Check if the block is SHA256 block
                if block_header_id == SHA_256_HEADER_ID:
                    if block_data_size >= 2:
                        data_bytes = local_extra_data[i+4 : current_block_end]

                        # Check internal signature
                        internal_sig = struct.unpack('<H', data_bytes[0:2])[0]
                        if internal_sig == SHA_256_HEADER_SIGNATURE:
                            found_sha_in_file = data_bytes[2:]
                            break

                i += (4 + block_data_size)

            if found_sha_in_file:
                sha256_checksums[member_info.filename] = found_sha_in_file
            else:
                print(f"{member_info.filename} sha not found", file=sys.stderr)

    return sha256_checksums

def find_build_id() -> str | None:
    tag_file_path = os.path.join(os.getenv('OUT_DIR', 'out'), 'file_name_tag.txt')
    build_id = None

    with open(tag_file_path, 'r', encoding='utf-8') as f:
            build_id = f.read().strip()

    return build_id

SHA_DIFF_ALLOWLIST = {
    "IMAGES/init_boot.img",
    "IMAGES/odm_dlkm.img",
    "IMAGES/odm.img",
    "IMAGES/product.img",
    "IMAGES/product.map",
    "IMAGES/system_dlkm.img",
    "IMAGES/system_ext.img",
    "IMAGES/system.img",
    "IMAGES/system.map",
    "IMAGES/system_other.img",
    "IMAGES/userdata.img",
    "IMAGES/vbmeta.img",
    "IMAGES/vbmeta_system_dlkm.img",
    "IMAGES/vbmeta_system.img",
    "IMAGES/vbmeta_vendor_dlkm.img",
    "IMAGES/vendor_boot.img",
    "IMAGES/vendor_dlkm.img",
    "IMAGES/vendor.img",
    "IMAGES/vendor.map",
    "META/care_map.pb",
    "META/file_contexts.bin",
    "META/kernel_version.txt",
    "META/misc_info.txt",
    "META/vbmeta_digest.txt",
    "ODM_DLKM/etc/NOTICE.xml.gz",
    "ODM/etc/NOTICE.xml.gz",
    "PRODUCT/etc/NOTICE.xml.gz",
    "SYSTEM_DLKM/etc/NOTICE.xml.gz",
    "SYSTEM/etc/NOTICE.xml.gz",
    "SYSTEM_EXT/etc/NOTICE.xml.gz",
    "VENDOR_DLKM/etc/NOTICE.xml.gz",
    "VENDOR/etc/NOTICE.xml.gz",
    "SYSTEM_EXT/etc/vm/trusty_vm/trusty_security_vm.elf", # TODO: Make this hermetic
}

def compare_sha_maps(soong_only_map: dict[str, bytes], soong_plus_make_map: dict[str, bytes]) -> bool:
    """Compares two sha maps and reports any missing or different entries."""

    all_keys = list(soong_only_map.keys() | soong_plus_make_map.keys())
    all_identical = True
    for key in all_keys:
        if key in SHA_DIFF_ALLOWLIST:
            continue
        if key not in soong_only_map:
            print(f'{key} not found in soong only build target_files.zip', file=sys.stderr)
            all_identical = False
        if key not in soong_plus_make_map:
            print(f'{key} not found in soong plus make build target_files.zip', file=sys.stderr)
            all_identical = False
        if soong_only_map[key] != soong_plus_make_map[key]:
            print(f'{key} sha value differ between soong only build and soong plus make build', file=sys.stderr)
            all_identical = False

    return all_identical

def get_zip_sha_map(product: Product, soong_only: bool) -> dict[str, bytes]:
    """Runs the build and returns the map of entries to its SHA256 values of target_files.zip."""

    out_dir = os.getenv('OUT_DIR', 'out')

    build_type = "soong only" if soong_only else "soong plus make"

    build_success = run_build_target_files_zip(product, soong_only)
    if not build_success:
        with open(os.path.join(out_dir, 'build.log'), 'r') as f:
            print(f.read(), file=sys.stderr)
        sys.exit(f'{build_type} build failed')

    build_id = find_build_id()
    dist_dir = os.getenv('DIST_DIR', os.path.join(out_dir, 'dist'))
    target_files_zip = os.path.join(dist_dir, f'{product.product}-target_files-{build_id}.zip')
    zip_sha_map = get_local_file_sha256_fields(target_files_zip)
    if zip_sha_map is None:
        sys.exit("Could not construct sha map for target_files.zip entries for soong only build")

    return zip_sha_map

def parse_args():
  parser = argparse.ArgumentParser()
  parser.add_argument("product", help="target product name")
  return parser.parse_args()

def main():
    os.chdir(get_top())

    args = parse_args()

    product = Product(
      args.product,
      'trunk_staging',
      'userdebug',
    )

    soong_only_zip_sha_map = get_zip_sha_map(product, True)
    soong_plus_make_zip_sha_map = get_zip_sha_map(product, False)

    if not compare_sha_maps(soong_only_zip_sha_map, soong_plus_make_zip_sha_map):
        sys.exit("target_files.zip differ between soong only build and soong plus make build")

    print("target_files.zip are identical between soong only build and soong plus make build")

if __name__ == "__main__":
    main()
