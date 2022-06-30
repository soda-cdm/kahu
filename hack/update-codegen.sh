#!/bin/bash
#
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

readonly ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
readonly CODE_GEN_PATH="${ROOT_DIR}/${OUT_DIR}/generators"
readonly KAHU_GO_PACKAGE=github.com/soda-cdm/kahu


(
export GOPATH=${CODE_GEN_PATH}
export GOBIN=${GOPATH}/bin

# clone code-generator
if [[ ! -d "${CODE_GEN_PATH}/code-generator" ]]; then
  mkdir -p "${CODE_GEN_PATH}"
  cd ${CODE_GEN_PATH}
  git clone -b ${CODE_GENERATOR_VERSION} https://github.com/kubernetes/code-generator
fi

# setup go env
go_pkg_dir="${GOPATH}/src/${KAHU_GO_PACKAGE}"
go_pkg_basedir=$(dirname "${go_pkg_dir}")

mkdir -p "${go_pkg_basedir}"

# TODO: This symlink should be relative.
if [[ ! -e "${go_pkg_dir}" || "$(readlink "${go_pkg_dir}")" != "${ROOT_DIR}" ]]; then
  ln -snf "${ROOT_DIR}" "${go_pkg_dir}"
fi

# generate the code with:
cd ${ROOT_DIR}
bash "${CODE_GEN_PATH}/code-generator/generate-groups.sh" \
  all \
  github.com/soda-cdm/kahu/client \
  github.com/soda-cdm/kahu/apis \
  "kahu:v1beta1" \
  --go-header-file ${ROOT_DIR}/hack/boilerplate.go.txt \
  $@
)
