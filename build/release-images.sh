#!/usr/bin/env bash

# Copyright 2022 The SODA Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

# The root of the build/dist directory
KAHU_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"

# copy and add restic binary in bin dir
RESTIC_BIN_DIR=${KAHU_ROOT}/_output/bin
if [ ! -f ${RESTIC_BIN_DIR}/restic ];then
    curl -L https://github.com/restic/restic/releases/download/v0.15.2/restic_0.15.2_linux_amd64.bz2 -o ${RESTIC_BIN_DIR}/restic.bz2
    bzip2 -df ${RESTIC_BIN_DIR}/restic.bz2
    chmod u+x ${RESTIC_BIN_DIR}/restic
fi

source "${KAHU_ROOT}/build/lib/golang.sh"
source "${KAHU_ROOT}/build/lib/release.sh"

release::package_tarballs
