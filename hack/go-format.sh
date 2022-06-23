#!/bin/bash
#
# Copyright 2022 The SODA Authors.
# Copyright 2017 the Velero contributors.
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

if [[ ${1:-} == 'verify' ]]; then
  MODE='-d'
else
  MODE='-w'
fi

files="$(find . -type f -name '*.go' -not -path './.go/*' -not -path '*.pb.go' -not -path './.proto/*' -not -path './vendor/*' -not -path './site/*' -not -path '*/client/*' -not -name 'zz_generated*' -not -path '*/mocks/*')"
echo "Checking gofmt"
for file in ${files}; do
  output=$(gofmt "${MODE}" -s "${file}")
  if [[ -n "${output}" ]]; then
    echo "${output}"
    echo "gofmt - failed!"
    exit 1
  fi
done

pkgs=$(go list ./...)
echo "Checking go vet"
for pkg in ${pkgs}; do
  output=$(go vet "${pkg}")
  if [[ -n "${output}" ]]; then
    echo "${output}"
    echo "go vet - failed!"
    exit 1
  fi
done

echo "Checking goimports"
if ! command -v goimports > /dev/null; then
  echo 'goimports is missing - please run "go install golang.org/x/tools/cmd/goimports"'
  exit 1
fi

for file in ${files}; do
  output=$(goimports "${MODE}" -local github.com/soda-cdm/kahu "${file}")
  if [[ -n "${output}" ]]; then
    echo "${output}"
    echo "goimports - failed!"
    exit 1
  fi
done

