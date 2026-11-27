#!/usr/bin/env bash
# Start or stop an ephemeral OpenWrt container (21.02–25.12) with a bind-mounted
# workspace for live bidirectional /sbin/uci golden integration tests.
#
#   ./scripts/openwrt-lab.sh start <21-02|22-03|23-05|24-10|25-12> <work_dir>
#   ./scripts/openwrt-lab.sh stop <work_dir>
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"

usage() {
  echo "usage: openwrt-lab.sh start <21-02|22-03|23-05|24-10|25-12> <work_dir> | stop <work_dir>" >&2
  exit 1
}

stop_resources() {
  local work_dir="$1"
  if [ -f "$work_dir/container.name" ]; then
    local cname
    cname="$(cat "$work_dir/container.name" 2>/dev/null || true)"
    if [ -n "$cname" ]; then
      docker rm -f "$cname" >/dev/null 2>&1 || true
    fi
    rm -f "$work_dir/container.name"
  fi
}

cmd_stop() {
  local raw_dir="${1:-}"
  [ -n "$raw_dir" ] || usage
  if [ ! -d "$raw_dir" ]; then
    return 0
  fi
  local work_dir
  work_dir="$(cd "$raw_dir" && pwd)"
  stop_resources "$work_dir"
  if [ -n "$work_dir" ] && [ "$work_dir" != "/" ]; then
    rm -rf "$work_dir"
  fi
}

resolve_release() {
  case "$1" in
    21-02|openwrt-21-02|21.02)
      IMAGE_KEY="openwrt-21-02"
      RELEASE_VER="21.02"
      ;;
    22-03|openwrt-22-03|22.03)
      IMAGE_KEY="openwrt-22-03"
      RELEASE_VER="22.03"
      ;;
    23-05|openwrt-23-05|23.05)
      IMAGE_KEY="openwrt-23-05"
      RELEASE_VER="23.05"
      ;;
    24-10|openwrt-24-10|24.10)
      IMAGE_KEY="openwrt-24-10"
      RELEASE_VER="24.10"
      ;;
    25-12|openwrt-25-12|25.12)
      IMAGE_KEY="openwrt-25-12"
      RELEASE_VER="25.12"
      ;;
    *)
      echo "unsupported OpenWrt target: $1 (expected 21-02, 22-03, 23-05, 24-10, or 25-12)" >&2
      exit 1
      ;;
  esac
}

cmd_start() {
  local target="${1:-}"
  local raw_dir="${2:-}"
  [ -n "$target" ] && [ -n "$raw_dir" ] || usage

  resolve_release "$target"
  mkdir -p "$raw_dir"
  local work_dir
  work_dir="$(cd "$raw_dir" && pwd)"
  stop_resources "$work_dir"

  local image_ref="${OPENWRT_LAB_IMAGE:-$("$here/ci-image.sh" "$IMAGE_KEY")}"
  mkdir -p "$work_dir/uci"

  local suffix
  suffix="$(LC_ALL=C tr -dc 'a-z0-9' </dev/urandom | head -c 8 || echo "$$")"
  local container_name="gouci-${IMAGE_KEY}-${suffix}"
  echo "$container_name" > "$work_dir/container.name"

  # Run as the host UID:GID so files created by /sbin/uci inside /uci-lab remain
  # readable and cleanable with 0600 permissions by the host Go test process.
  docker run -d --platform linux/amd64 \
    --name "$container_name" \
    --user "$(id -u):$(id -g)" \
    -v "$work_dir/uci:/uci-lab" \
    "$image_ref" \
    /bin/sh -c 'trap "exit 0" TERM INT; while :; do sleep 60 & wait $!; done' >/dev/null

  # Verify /sbin/uci is runnable inside the container.
  if ! docker exec "$container_name" /sbin/uci -h >/dev/null 2>&1; then
    docker logs "$container_name" >&2 || true
    stop_resources "$work_dir"
    exit 1
  fi

  cat > "$work_dir/env.sh" <<EOF
export UCI_TEST_CONTAINER='$container_name'
export UCI_TEST_HOST_DIR='$work_dir/uci'
export UCI_TEST_CONTAINER_DIR='/uci-lab'
export UCI_TEST_RELEASE='$RELEASE_VER'
EOF

  echo "$work_dir/env.sh"
}

action="${1:-}"
[ -n "$action" ] || usage
shift

case "$action" in
  start) cmd_start "$@" ;;
  stop) cmd_stop "$@" ;;
  *) usage ;;
esac
