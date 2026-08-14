#!/usr/bin/env bash

set -Eeuo pipefail

RELEASE_TAG="${RELEASE_TAG:?RELEASE_TAG is required}"
TARGET_SHA="${TARGET_SHA:-HEAD}"
REPOSITORY="${GITHUB_REPOSITORY:-Snail-one/MonitorKit}"
OUTPUT_PATH="${1:-release-notes.md}"

if git rev-parse --verify --quiet "refs/tags/${RELEASE_TAG}" >/dev/null; then
  TARGET="refs/tags/${RELEASE_TAG}"
else
  TARGET="${TARGET_SHA}"
fi

PREVIOUS_TAG="$(
  git tag --merged "${TARGET}" --list 'v*' --sort=-version:refname |
    awk -v current="${RELEASE_TAG}" '$0 != current && !found { print; found = 1 }'
)"

if [[ -n "${PREVIOUS_TAG}" ]]; then
  RANGE="${PREVIOUS_TAG}..${TARGET}"
else
  RANGE="${TARGET}"
fi

print_commit_notes() {
  local commit="$1"
  local short_commit="${commit:0:7}"
  local line cleaned
  local -a lines=()
  local index

  while IFS= read -r line; do
    cleaned="$(
      printf '%s' "${line}" |
        sed -e 's/^[[:space:]]*//' \
          -e 's/^[•*-][[:space:]]*//' \
          -e 's/[[:space:]]*$//'
    )"
    if [[ -n "${cleaned}" ]]; then
      lines+=("${cleaned}")
    fi
  done < <(git show --no-patch --format='%B' "${commit}")

  if [[ "${#lines[@]}" -eq 0 ]]; then
    lines=("无标题提交")
  fi

  printf -- '- %s ([`%s`](https://github.com/%s/commit/%s))\n' \
    "${lines[0]}" "${short_commit}" "${REPOSITORY}" "${commit}"
  for ((index = 1; index < ${#lines[@]}; index++)); do
    printf -- '  - %s\n' "${lines[$index]}"
  done
}

{
  printf '## 本次更新\n\n'
  FIRST_COMMIT="$(git rev-list --max-count=1 --no-merges "${RANGE}")"
  if [[ -n "${FIRST_COMMIT}" ]]; then
    while IFS= read -r commit; do
      print_commit_notes "${commit}"
    done < <(git rev-list --no-merges "${RANGE}")
  else
    printf '本版本未检测到新的提交。\n'
  fi
} >"${OUTPUT_PATH}"

printf '发布说明已生成：%s\n' "${OUTPUT_PATH}"
