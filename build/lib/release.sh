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
readonly LOCAL_OUTPUT="${KAHU_ROOT}/${OUT_DIR:-_output}"
readonly LOCAL_OUTPUT_BINPATH="${LOCAL_OUTPUT}/bin"
readonly RELEASE_IMAGES="${LOCAL_OUTPUT}/release-images"
readonly RELEASE_STAGE="${LOCAL_OUTPUT}/release-stage"

# Get the set of master binaries that run in Docker (on Linux)
# Entry format is "<binary-name>,<base-image>".
# Binaries are placed in /usr/local/bin inside the image.
# `make` users can override any or all of the base images using the associated
# environment variables.
#
# $1 - server architecture
build::get_docker_wrapped_binaries() {
  ### If you change any of these lists, please also update DOCKERIZED_BINARIES
  ### in build/BUILD. And golang::targets
  local targets=(
    controller-manager
    meta-service
    volume-service
    openebs-zfs
    nfs-provider
  )

  echo "${targets[@]}"
}


# Package up all of the binaries
function release::package_tarballs() {
  release::build_images
}

# ensure-docker
# Check if we have "docker" commands available
#
function ensure-docker {
  if docker --help >/dev/null 2>&1; then
    return 0
  else
    echo "ERROR: docker command not available. Docker 19.03 or higher is required"
    exit 1
  fi
}


# Package up all of the server binaries in docker images
function release::build_images() {
  ensure-docker

  # Clean out any old images
  rm -rf "${RELEASE_IMAGES}"

  local release_stage
  release_stage="${RELEASE_STAGE}/kahu"
  rm -rf "${release_stage}"
  mkdir -p "${release_stage}/bin"

  # This fancy expression will expand to prepend a path
  # (${LOCAL_OUTPUT_BINPATH}/) to every item in the
  # KAHU_IMAGE_BINARIES array.
  cp "${KAHU_IMAGE_BINARIES[@]/#/${LOCAL_OUTPUT_BINPATH}/${platform//\//_}/}" \
    "${release_stage}/bin/"

  release::create_docker_images "${release_stage}/bin"
}

# This builds all the release docker images (One docker image per binary)
# Args:
#  $1 - binary_dir, the directory to save the tared images to.
function release::create_docker_images() {
  # Create a sub-shell so that we don't pollute the outer environment
  (
    local binary_dir
    local binaries
    local images_dir
    binary_dir="$1"

    binaries=$(build::get_docker_wrapped_binaries)
    images_dir="${RELEASE_IMAGES}"
    mkdir -p "${images_dir}"

    local -r docker_registry=${BASE_IMAGE_REGISTRY}
    # Docker tags cannot contain '+'
    local docker_tag="${VERSION/+_}"
    if [[ -z "${docker_tag}" ]]; then
      log::error "git version information missing; cannot create Docker tag"
      return 1
    fi

    for binary_name in $binaries; do

      local binary_file_path="${binary_dir}/${binary_name}"
      local docker_build_path="${binary_file_path}.dockerbuild"
      local docker_image_tag="${docker_registry}/${binary_name}:${docker_tag}"

      local docker_file_path="${KAHU_ROOT}/Dockerfile"

      log::status "Starting docker build for image: ""${docker_image_tag}"
      (
        rm -rf "${docker_build_path}"
        mkdir -p "${docker_build_path}"
        ln "${binary_file_path}" "${docker_build_path}/${binary_name}"

        local build_log="${docker_build_path}/build.log"
        if ! docker build \
          -f "${docker_file_path}" \
          -t "${docker_image_tag}" \
          --build-arg BINARY="${binary_name}" \
          "${docker_build_path}" >"${build_log}" 2>&1; then
            cat "${build_log}"
            exit 1
        fi

        rm "${build_log}"

        docker save -o "${binary_file_path}-${docker_tag}.tar" "${docker_image_tag}"
        echo "${docker_tag}" > "${binary_file_path}.docker_tag"
        rm -rf "${docker_build_path}"
        ln "${binary_file_path}-${docker_tag}.tar" "${images_dir}/"

        log::status "Deleting docker image ${docker_image_tag}"
        docker rmi "${docker_image_tag}" &>/dev/null || true
      ) &
    done

    util::wait-for-jobs || { log::error "previous Docker build failed"; return 1; }
    log::status "Docker builds done"
  )

}

# Wait for background jobs to finish. Return with
# an error status if any of the jobs failed.
util::wait-for-jobs() {
  local fail=0
  local job
  for job in $(jobs -p); do
    wait "${job}" || fail=$((fail + 1))
  done
  return ${fail}
}
