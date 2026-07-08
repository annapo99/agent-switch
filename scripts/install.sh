#!/usr/bin/env sh
set -eu

repo="annapo99/agent-switch"
bin="ags"
version="${AGS_VERSION:-latest}"
install_dir="${AGS_INSTALL_DIR:-$HOME/.local/bin}"

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "agent-switch installer needs $1" >&2
    exit 1
  fi
}

need curl
need tar
need install

case "$(uname -s)" in
  Darwin) os="darwin" ;;
  Linux) os="linux" ;;
  *)
    echo "Unsupported OS: $(uname -s)" >&2
    exit 1
    ;;
esac

case "$(uname -m)" in
  arm64|aarch64) arch="arm64" ;;
  x86_64|amd64) arch="amd64" ;;
  *)
    echo "Unsupported architecture: $(uname -m)" >&2
    exit 1
    ;;
esac

asset="${bin}_${os}_${arch}.tar.gz"
if [ -n "${AGS_RELEASE_BASE_URL:-}" ]; then
  base_url="${AGS_RELEASE_BASE_URL%/}"
elif [ "$version" = "latest" ]; then
  base_url="https://github.com/${repo}/releases/latest/download"
else
  base_url="https://github.com/${repo}/releases/download/${version}"
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT INT TERM

archive="${tmp_dir}/${asset}"
checksums="${tmp_dir}/checksums.txt"

echo "Downloading ${repo} ${version} for ${os}/${arch}..."
curl -fsSL "${base_url}/${asset}" -o "$archive"
curl -fsSL "${base_url}/checksums.txt" -o "$checksums"

expected="$(grep " ${asset}$" "$checksums" || true)"
if [ -z "$expected" ]; then
  echo "Checksum not found for ${asset}" >&2
  exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
  (cd "$tmp_dir" && printf '%s\n' "$expected" | sha256sum -c -)
elif command -v shasum >/dev/null 2>&1; then
  (cd "$tmp_dir" && printf '%s\n' "$expected" | shasum -a 256 -c -)
else
  echo "Checksum verification needs sha256sum or shasum" >&2
  exit 1
fi

tar -xzf "$archive" -C "$tmp_dir"
mkdir -p "$install_dir"
install -m 0755 "${tmp_dir}/${bin}" "${install_dir}/${bin}"

echo "Installed ${bin} to ${install_dir}/${bin}"
case ":$PATH:" in
  *":${install_dir}:"*) ;;
  *) echo "Add ${install_dir} to PATH to run ${bin} from any shell." ;;
esac
