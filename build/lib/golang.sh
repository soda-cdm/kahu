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

KAHU_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
source "${KAHU_ROOT}/build/lib/logging.sh"

readonly KAHU_GOPATH="${KAHU_ROOT}/${OUT_DIR}/go"
readonly KAHU_GO_PACKAGE=github.com/soda-cdm/kahu
readonly minimum_go_version=go1.17.0
readonly KAHU_STATIC_BINARIES=(
  # add statically linked binaries here
  controller-manager
  meta-service
  volume-service
  openebs-zfs
  nfs-provider
)

golang::targets() {
  # add all golang binaries here
  local targets=(
    cmd/controller-manager
    cmd/meta-service
    cmd/volume-service
    providers/nfs/nfs-provider
    cmd/openebs-zfs
  )
  echo "${targets[@]}"
}

IFS=" " read -ra KAHU_GOLANG_TARGETS <<< "$(golang::targets)"
readonly KAHU_GOLANG_TARGETS
readonly KAHU_IMAGE_BINARIES=("${KAHU_GOLANG_TARGETS[@]##*/}")

readonly KAHU_ALL_TARGETS=(
  "${KAHU_GOLANG_TARGETS[@]}"
)

golang::is_statically_linked_binary() {
  local e
  for e in "${KAHU_STATIC_BINARIES[@]}"; do [[ "${1}" == *"/${e}" ]] && return 0; done;
  return 1;
}

# Create the GOPATH tree under $KAHU_OUTPUT
golang::create_gopath_tree() {
  local go_pkg_dir="${KAHU_GOPATH}/src/${KAHU_GO_PACKAGE}"
  local go_pkg_basedir
  go_pkg_basedir=$(dirname "${go_pkg_dir}")

  mkdir -p "${go_pkg_basedir}"

  # TODO: This symlink should be relative.
  if [[ ! -e "${go_pkg_dir}" || "$(readlink "${go_pkg_dir}")" != "${KAHU_ROOT}" ]]; then
    ln -snf "${KAHU_ROOT}" "${go_pkg_dir}"
  fi
}

# binaries_from_targets take a list of build targets and return the
# full go package to be built
golang::binaries_from_targets() {
  local target
  for target; do
    # If the target starts with what looks like a domain name, assume it has a
    # fully-qualified package name rather than one that needs the KAHU
    # package prepended.
    if [[ "${target}" =~ ^([[:alnum:]]+".")+[[:alnum:]]+"/" ]]; then
      echo "${target}"
    else
      echo "${KAHU_GO_PACKAGE}/${target}"
    fi
  done
}

# Ensure the go and minimum version.
golang::verify_go_version() {
  if [[ -z "$(command -v go)" ]]; then
    echo "
Can't find 'go' in PATH, please fix and retry.
See http://golang.org/doc/install for installation instructions.
"
    return 2
  fi

  local go_version
  IFS=" " read -ra go_version <<< "$(GOFLAGS='' go version)"

  if [[ "${minimum_go_version}" != $(echo -e "${minimum_go_version}\n${go_version[2]}" | sort -s -t. -k 1,1 -k 2,2n -k 3,3n | head -n1) && "${go_version[2]}" != "devel" ]]; then
    echo"
Detected go version: ${go_version[*]}.
Kahu requires ${minimum_go_version} or greater.
Please install ${minimum_go_version} or later.
"
    return 2
  fi
}

golang::build_binaries() {
  # Create a sub-shell so that we don't pollute the outer environment
  (
    golang::verify_go_version

    golang::create_gopath_tree

    export GOPATH="${KAHU_GOPATH}"
    export GOCACHE="${KAHU_GOPATH}/cache"
    export GOARCH="${ARCH}"
    export GOOS="${OS}"
    unset GOBIN
    export GO15VENDOREXPERIMENT=1

    local goflags goldflags goasmflags gogcflags gotags

    goldflags="all=$(build::ldflags) ${GOLDFLAGS:-}"
    if [[ "${DBG:-}" != 1 ]]; then
        # Not debugging - disable symbols and DWARF.
        goldflags="${goldflags} -s -w"
    fi

    goasmflags="-trimpath=${KAHU_ROOT}"
    gogcflags="${GOGCFLAGS:-} -trimpath=${KAHU_ROOT}"

    # extract tags if any specified in GOFLAGS
    # shellcheck disable=SC2001
    gotags="selinux,notest,$(echo "${GOFLAGS:-}" | sed -e 's|.*-tags=\([^-]*\).*|\1|')"

    local -a targets=()

    for arg; do
      if [[ "${arg}" == -* ]]; then
        # Assume arguments starting with a dash are flags.
        goflags+=("${arg}")
      else
        targets+=("${arg}")
      fi
    done

    if [[ ${#targets[@]} -eq 0 ]]; then
      targets=("${KAHU_ALL_TARGETS[@]}")
    fi

    local -a binaries
    while IFS="" read -r binary; do binaries+=("$binary"); done < <(golang::binaries_from_targets "${targets[@]}")

    V=2 log::info "Building go targets:" "${targets[@]}"
    (
      umask 0022
      local -a statics=()
      local -a nonstatics=()

      for binary in "${binaries[@]}"; do
        if golang::is_statically_linked_binary "${binary}"; then
          statics+=("${binary}")
        else
          nonstatics+=("${binary}")
        fi
      done

      local -a build_args
      if [[ "${#statics[@]}" != 0 ]]; then
        build_args=(
          -installsuffix static
          ${goflags:+"${goflags[@]}"}
          -gcflags "${gogcflags:-}"
          -asmflags "${goasmflags:-}"
          -ldflags "${goldflags:-}"
          -tags "${gotags:-}"
        )
        V=2 log::info "Building static binary ..." "${statics[@]}"
        CGO_ENABLED=0 golang::build_some_binaries "${statics[@]}"
      fi

      if [[ "${#nonstatics[@]}" != 0 ]]; then
        build_args=(
          ${goflags:+"${goflags[@]}"}
          -gcflags="${gogcflags}"
          -asmflags="${goasmflags}"
          -ldflags="${goldflags}"
          -tags="${gotags:-}"
        )
        V=2 log::info "Building nonstatic binary ..." "${nonstatics[@]}"
        CGO_ENABLED=1 golang::build_some_binaries "${nonstatics[@]}"
      fi
    )
  )
}

golang::build_some_binaries() {
  if [[ -n "${KAHU_BUILD_WITH_COVERAGE:-}" ]]; then
    local -a uncovered=()
    for package in "$@"; do
      if golang::is_instrumented_package "${package}"; then
        V=2 log::info "Building ${package} with coverage..."

        golang::create_coverage_dummy_test "${package}"
        util::trap_add "golang::delete_coverage_dummy_test \"${package}\"" EXIT

        go test -c -o "$(golang::outdir_for_binary "${package}" "${platform}")" \
          -covermode count \
          -coverpkg github.com/soda-cdm/kahu/... \
          "${build_args[@]}" \
          -tags coverage \
          "${package}"
      else
        uncovered+=("${package}")
      fi
    done
    if [[ "${#uncovered[@]}" != 0 ]]; then
      V=2 log::info "Building ${uncovered[*]} without coverage..."
      go install "${build_args[@]}" "${uncovered[@]}"
    else
      V=2 log::info "Nothing to build without coverage."
     fi
   else
    V=2 log::info "Coverage is disabled."
    go install "${build_args[@]}" "$@"
   fi
}

# This will take binaries from $GOPATH/bin and copy them to the appropriate
# place in ${KAHU_OUTPUT_BINPATH}/${PLATFORM}
golang::place_bins() {
  log::status "Placing binaries in ${KAHU_OUTPUT_BINPATH}/${platform//\//_}"

  local full_binpath_src=$(golang::outdir_for_binary "${platform}")
  if [[ -d "${full_binpath_src}" ]]; then
    mkdir -p "${KAHU_OUTPUT_BINPATH}/${platform//\//_}"
    find "${full_binpath_src}" -maxdepth 1 -type f -exec \
      rsync -pc {} "${KAHU_OUTPUT_BINPATH}/${platform//\//_}" \;
    cp "${KAHU_IMAGE_BINARIES[@]/#/${KAHU_OUTPUT_BINPATH}/${platform//\//_}/}" \
      "${KAHU_OUTPUT_BINPATH}/"
  fi

}

# Prints the value that needs to be passed to the -ldflags parameter of go build
build::ldflags() {
  local -a ldflags
  local buildDate
  buildDate="$(date ${SOURCE_DATE_EPOCH:+"--date=@${SOURCE_DATE_EPOCH}"} -u +'%Y-%m-%dT%H:%M:%SZ')"
  ldflags+=(
        "-X '${KAHU_GO_PACKAGE}/utils.gitVersion=${VERSION}'"
        "-X '${KAHU_GO_PACKAGE}/utils.gitCommit=${GIT_SHA}'"
        "-X '${KAHU_GO_PACKAGE}/utils.gitTreeState=${GIT_TREE_STATE}'"
        "-X '${KAHU_GO_PACKAGE}/utils.buildDate=${buildDate}'"
      )

  echo "${ldflags[*]-}"
}

golang::host_platform() {
  echo "$(go env GOHOSTOS)/$(go env GOHOSTARCH)"
}

golang::outdir_for_binary() {
  local platform=$1
  host_platform=$(golang::host_platform)
  local output_path="${KAHU_GOPATH}/bin"

  if [[ "${platform}" != "${host_platform}" ]]; then
    output_path="${output_path}/${platform//\//_}"
  fi

  echo "${output_path}"
}
