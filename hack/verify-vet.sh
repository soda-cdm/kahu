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

KAHU_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
source "${KAHU_ROOT}/build/lib/logging.sh"

pkgs=$(go list ./...)
log::status "Checking go vet"
for pkg in ${pkgs}; do
  output=$(go vet "${pkg}")
  if [[ -n "${output}" ]]; then
    log::error "${output}"
    log::error "go vet - failed!"
    exit 1
  fi
done