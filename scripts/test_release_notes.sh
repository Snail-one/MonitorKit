#!/usr/bin/env bash

set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_DIR="$(mktemp -d)"
trap 'rm -rf "${TEST_DIR}"' EXIT

git -C "${TEST_DIR}" init -q
git -C "${TEST_DIR}" config user.name "MonitorKit Test"
git -C "${TEST_DIR}" config user.email "monitorkit-test@example.invalid"
git -C "${TEST_DIR}" config commit.gpgsign false

printf 'fixture\n' >"${TEST_DIR}/fixture.txt"
git -C "${TEST_DIR}" add fixture.txt
git -C "${TEST_DIR}" commit -q -m $'发布流程优化\n\n- 自动测试\n- 生成校验文件'
git -C "${TEST_DIR}" tag v0.0.1

(
  cd "${TEST_DIR}"
  RELEASE_TAG=v0.0.1 \
    GITHUB_REPOSITORY=Snail-one/MonitorKit \
    bash "${ROOT_DIR}/scripts/generate_release_notes.sh" "${TEST_DIR}/release-notes.md"
)

grep -Fq -- '- 发布流程优化 (' "${TEST_DIR}/release-notes.md"
grep -Fq -- '  - 自动测试' "${TEST_DIR}/release-notes.md"
grep -Fq -- '  - 生成校验文件' "${TEST_DIR}/release-notes.md"

printf '发布说明结构回归测试通过\n'
