#!/usr/bin/env bash

set -euo pipefail

SCRIPT_NAME=$(basename "$0")

usage() {
  cat <<'EOF'
Usage:
  scripts/deploy-remote.sh --target ssh --ssh-host HOST [options]
  scripts/deploy-remote.sh --target container --container NAME [options]

Build the current repo on a remote machine or inside a running container, then
install the resulting binary in a temporary or permanent location.

Options:
  --target ssh|container        Deployment backend.

SSH target options:
  --ssh-host HOST               SSH destination host.
  --ssh-user USER               Optional SSH user.
  --ssh-port PORT               Optional SSH port.

Container target options:
  --container NAME              Running container name or ID.
  --container-runtime CMD       Container runtime. Default: docker

Install options:
  --install-mode temporary      Install into the remote staging directory.
  --install-mode permanent      Install into a persistent prefix. Default.
  --install-dir DIR             Install prefix. Default: \$HOME/.local/bin
  --sudo-install                Use sudo for the final install step.
  --bin-name NAME               Output binary name. Default: cr

Build options:
  --workdir DIR                 Remote staging directory. Default: mktemp.
  --go-bin PATH                 Go executable on the remote side. Default: go
  --build-target PKG            Go build target. Default: ./cmd/cr
  --build-flags FLAGS           Extra flags passed to go build.
  --verify                      Run go test ./... remotely before install.

Transfer options:
  --exclude PATTERN             Extra tar exclude pattern. May be repeated.
  --keep-workdir                Leave the remote staging directory in place.
  --dry-run                     Print the plan without executing it.
  --help                        Show this help.

Examples:
  scripts/deploy-remote.sh --target ssh --ssh-host app.example.com
  scripts/deploy-remote.sh --target ssh --ssh-user dc --ssh-host app.example.com --install-dir /usr/local/bin --sudo-install
  scripts/deploy-remote.sh --target container --container cr-dev --install-mode temporary
EOF
}

die() {
  echo "$SCRIPT_NAME: $*" >&2
  exit 1
}

quote_sh() {
  printf "'%s'" "${1//\'/\'\\\'\'}"
}

TARGET=""
SSH_HOST=""
SSH_USER=""
SSH_PORT=""
CONTAINER_NAME=""
CONTAINER_RUNTIME="docker"
INSTALL_MODE="permanent"
INSTALL_DIR='$HOME/.local/bin'
INSTALL_DIR_IS_DEFAULT=1
SUDO_INSTALL=0
BIN_NAME="cr"
WORKDIR=""
GO_BIN="go"
BUILD_TARGET="./cmd/cr"
BUILD_FLAGS=""
VERIFY=0
KEEP_WORKDIR=0
DRY_RUN=0
EXCLUDES=(".git" "cr")

while [[ $# -gt 0 ]]; do
  case "$1" in
    --target)
      [[ $# -ge 2 ]] || die "missing value for $1"
      TARGET="$2"
      shift 2
      ;;
    --ssh-host)
      [[ $# -ge 2 ]] || die "missing value for $1"
      SSH_HOST="$2"
      shift 2
      ;;
    --ssh-user)
      [[ $# -ge 2 ]] || die "missing value for $1"
      SSH_USER="$2"
      shift 2
      ;;
    --ssh-port)
      [[ $# -ge 2 ]] || die "missing value for $1"
      SSH_PORT="$2"
      shift 2
      ;;
    --container)
      [[ $# -ge 2 ]] || die "missing value for $1"
      CONTAINER_NAME="$2"
      shift 2
      ;;
    --container-runtime)
      [[ $# -ge 2 ]] || die "missing value for $1"
      CONTAINER_RUNTIME="$2"
      shift 2
      ;;
    --install-mode)
      [[ $# -ge 2 ]] || die "missing value for $1"
      INSTALL_MODE="$2"
      shift 2
      ;;
    --install-dir)
      [[ $# -ge 2 ]] || die "missing value for $1"
      INSTALL_DIR="$2"
      INSTALL_DIR_IS_DEFAULT=0
      shift 2
      ;;
    --sudo-install)
      SUDO_INSTALL=1
      shift
      ;;
    --bin-name)
      [[ $# -ge 2 ]] || die "missing value for $1"
      BIN_NAME="$2"
      shift 2
      ;;
    --workdir)
      [[ $# -ge 2 ]] || die "missing value for $1"
      WORKDIR="$2"
      shift 2
      ;;
    --go-bin)
      [[ $# -ge 2 ]] || die "missing value for $1"
      GO_BIN="$2"
      shift 2
      ;;
    --build-target)
      [[ $# -ge 2 ]] || die "missing value for $1"
      BUILD_TARGET="$2"
      shift 2
      ;;
    --build-flags)
      [[ $# -ge 2 ]] || die "missing value for $1"
      BUILD_FLAGS="$2"
      shift 2
      ;;
    --verify)
      VERIFY=1
      shift
      ;;
    --exclude)
      [[ $# -ge 2 ]] || die "missing value for $1"
      EXCLUDES+=("$2")
      shift 2
      ;;
    --keep-workdir)
      KEEP_WORKDIR=1
      shift
      ;;
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

[[ -n "$TARGET" ]] || die "--target is required"
[[ "$INSTALL_MODE" == "temporary" || "$INSTALL_MODE" == "permanent" ]] || die "--install-mode must be temporary or permanent"

case "$TARGET" in
  ssh)
    [[ -n "$SSH_HOST" ]] || die "--ssh-host is required when --target ssh"
    ;;
  container)
    [[ -n "$CONTAINER_NAME" ]] || die "--container is required when --target container"
    ;;
  *)
    die "--target must be ssh or container"
    ;;
esac

if [[ "$INSTALL_MODE" == "temporary" && "$INSTALL_DIR" == '$HOME/.local/bin' ]]; then
  INSTALL_DIR=""
fi

REMOTE_TARGET_DESC=""
SSH_DEST=""

run_remote() {
  local cmd="$1"

  if [[ "$DRY_RUN" -eq 1 ]]; then
    printf '[dry-run] remote exec on %s: %s\n' "$REMOTE_TARGET_DESC" "$cmd"
    return 0
  fi

  case "$TARGET" in
    ssh)
      local args=()
      [[ -n "$SSH_PORT" ]] && args+=("-p" "$SSH_PORT")
      args+=("$SSH_DEST" "sh" "-lc" "$cmd")
      ssh "${args[@]}"
      ;;
    container)
      "$CONTAINER_RUNTIME" exec -i "$CONTAINER_NAME" sh -lc "$cmd"
      ;;
  esac
}

stream_repo() {
  local tar_args=(-czf -)
  local exclude
  for exclude in "${EXCLUDES[@]}"; do
    tar_args+=("--exclude=$exclude")
  done
  tar_args+=(".")

  if [[ "$DRY_RUN" -eq 1 ]]; then
    printf '[dry-run] transfer repo to %s with tar excludes: %s\n' "$REMOTE_TARGET_DESC" "${EXCLUDES[*]}"
    return 0
  fi

  case "$TARGET" in
    ssh)
      local ssh_args=()
      [[ -n "$SSH_PORT" ]] && ssh_args+=("-p" "$SSH_PORT")
      ssh_args+=("$SSH_DEST" "mkdir -p $(quote_sh "$REMOTE_WORKDIR") && tar -xzf - -C $(quote_sh "$REMOTE_WORKDIR")")
      tar "${tar_args[@]}" | ssh "${ssh_args[@]}"
      ;;
    container)
      tar "${tar_args[@]}" | "$CONTAINER_RUNTIME" exec -i "$CONTAINER_NAME" sh -lc "mkdir -p $(quote_sh "$REMOTE_WORKDIR") && tar -xzf - -C $(quote_sh "$REMOTE_WORKDIR")"
      ;;
  esac
}

if [[ "$TARGET" == "ssh" ]]; then
  SSH_DEST="$SSH_HOST"
  if [[ -n "$SSH_USER" ]]; then
    SSH_DEST="${SSH_USER}@${SSH_HOST}"
  fi
  REMOTE_TARGET_DESC="ssh:${SSH_DEST}"
else
  REMOTE_TARGET_DESC="container:${CONTAINER_NAME}"
fi

if [[ -n "$WORKDIR" ]]; then
  REMOTE_WORKDIR="$WORKDIR"
else
  if [[ "$DRY_RUN" -eq 1 ]]; then
    REMOTE_WORKDIR="<mktemp>"
  else
    REMOTE_WORKDIR=$(run_remote "mktemp -d 2>/dev/null || mktemp -d -t codereview-deploy")
    REMOTE_WORKDIR=${REMOTE_WORKDIR//$'\r'/}
    REMOTE_WORKDIR=${REMOTE_WORKDIR//$'\n'/}
  fi
fi

if [[ "$INSTALL_MODE" == "temporary" ]]; then
  REMOTE_INSTALL_DIR_DISPLAY="$REMOTE_WORKDIR/bin"
  REMOTE_INSTALL_DIR_EXPR=$(quote_sh "$REMOTE_WORKDIR/bin")
else
  if [[ "$INSTALL_DIR_IS_DEFAULT" -eq 1 ]]; then
    REMOTE_INSTALL_DIR_DISPLAY='$HOME/.local/bin'
    REMOTE_INSTALL_DIR_EXPR='$HOME/.local/bin'
  else
    REMOTE_INSTALL_DIR_DISPLAY="$INSTALL_DIR"
    REMOTE_INSTALL_DIR_EXPR=$(quote_sh "$INSTALL_DIR")
  fi
fi

REMOTE_BUILD_DIR="$REMOTE_WORKDIR/.build"
REMOTE_BINARY="$REMOTE_BUILD_DIR/$BIN_NAME"
REMOTE_INSTALLED_BINARY_DISPLAY="$REMOTE_INSTALL_DIR_DISPLAY/$BIN_NAME"

BUILD_FLAGS_STRIPPED=$(printf '%s' "$BUILD_FLAGS" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
VERIFY_CMD=""
if [[ "$VERIFY" -eq 1 ]]; then
  VERIFY_CMD="$(quote_sh "$GO_BIN") test ./... && "
fi

INSTALL_PREFIX=""
if [[ "$SUDO_INSTALL" -eq 1 ]]; then
  INSTALL_PREFIX="sudo "
fi

PREPARE_CMD=$(
  cat <<EOF
set -e
mkdir -p $(quote_sh "$REMOTE_BUILD_DIR")
command -v $(quote_sh "$GO_BIN") >/dev/null 2>&1
command -v git >/dev/null 2>&1
EOF
)

BUILD_CMD=$(
  cat <<EOF
set -e
cd $(quote_sh "$REMOTE_WORKDIR")
${VERIFY_CMD}$(quote_sh "$GO_BIN") build -trimpath ${BUILD_FLAGS_STRIPPED} -o $(quote_sh "$REMOTE_BINARY") $(quote_sh "$BUILD_TARGET")
EOF
)

INSTALL_CMD=$(
  cat <<EOF
set -e
install_dir=${REMOTE_INSTALL_DIR_EXPR}
installed_binary="\$install_dir/$(printf '%s' "$BIN_NAME")"
${INSTALL_PREFIX}mkdir -p "\$install_dir"
if command -v install >/dev/null 2>&1; then
  ${INSTALL_PREFIX}install -m 0755 $(quote_sh "$REMOTE_BINARY") "\$installed_binary"
else
  ${INSTALL_PREFIX}cp $(quote_sh "$REMOTE_BINARY") "\$installed_binary"
  ${INSTALL_PREFIX}chmod 0755 "\$installed_binary"
fi
printf '%s\n' "\$installed_binary"
EOF
)

CLEANUP_CMD=""
if [[ "$KEEP_WORKDIR" -eq 0 && "$INSTALL_MODE" == "permanent" ]]; then
  CLEANUP_CMD=$(
    cat <<EOF
set -e
rm -rf $(quote_sh "$REMOTE_WORKDIR")
EOF
  )
fi

echo "Deploy target: $REMOTE_TARGET_DESC"
echo "Remote workdir: $REMOTE_WORKDIR"
echo "Install mode: $INSTALL_MODE"
echo "Install path: $REMOTE_INSTALLED_BINARY_DISPLAY"

if [[ "$DRY_RUN" -eq 1 ]]; then
  echo
  echo "Remote prepare command:"
  printf '%s\n' "$PREPARE_CMD"
  echo
  echo "Repo transfer:"
  printf 'tar -czf -'
  for exclude in "${EXCLUDES[@]}"; do
    printf ' --exclude=%s' "$exclude"
  done
  printf ' .\n'
  echo
  echo "Remote build command:"
  printf '%s\n' "$BUILD_CMD"
  echo
  echo "Remote install command:"
  printf '%s\n' "$INSTALL_CMD"
  if [[ -n "$CLEANUP_CMD" ]]; then
    echo
    echo "Remote cleanup command:"
    printf '%s\n' "$CLEANUP_CMD"
  fi
  exit 0
fi

run_remote "$PREPARE_CMD" >/dev/null
stream_repo
run_remote "$BUILD_CMD" >/dev/null
INSTALLED_PATH=$(run_remote "$INSTALL_CMD")
INSTALLED_PATH=${INSTALLED_PATH//$'\r'/}
INSTALLED_PATH=${INSTALLED_PATH//$'\n'/}

if [[ -n "$CLEANUP_CMD" ]]; then
  run_remote "$CLEANUP_CMD" >/dev/null
fi

echo "Installed binary: $INSTALLED_PATH"
if [[ "$INSTALL_MODE" == "temporary" ]]; then
  echo "Temporary staging retained at: $REMOTE_WORKDIR"
fi
