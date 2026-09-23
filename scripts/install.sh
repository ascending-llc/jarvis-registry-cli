#!/usr/bin/env bash
set -euo pipefail

readonly release_api="https://api.github.com/repos/ascending-llc/jarvis-registry-cli/releases/latest"
readonly release_base="https://github.com/ascending-llc/jarvis-registry-cli/releases/download"

fail() {
    printf 'jarvis-registry installer: %s\n' "$1" >&2
    exit 1
}

require_command() {
    command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

resolve_tag() {
    local tag

    if [[ -n "${JARVIS_REGISTRY_VERSION:-}" ]]; then
        tag="v${JARVIS_REGISTRY_VERSION#v}"
    else
        curl -fsSL "$release_api" -o "$workdir/latest.json" ||
            fail "could not resolve the latest GitHub release"
        tag="$(sed -nE 's/^[[:space:]]*[{]?[[:space:]]*"tag_name":[[:space:]]*"([^"]+)".*/\1/p' "$workdir/latest.json")"
    fi

    [[ "$tag" =~ ^v[0-9][0-9A-Za-z.+-]*$ ]] ||
        fail "invalid release version: $tag"
    printf '%s\n' "$tag"
}

verify_archive() {
    local checksum_line

    checksum_line="$(awk -v filename="$archive" '
        NF == 2 && $2 == filename { line = $0; count++ }
        END { if (count == 1) print line; else exit 1 }
    ' "$workdir/checksums.txt")" ||
        fail "checksums.txt must contain exactly one entry for $archive"

    if command -v sha256sum >/dev/null 2>&1; then
        (cd "$workdir" && printf '%s\n' "$checksum_line" | sha256sum -c - >/dev/null) ||
            fail "SHA-256 verification failed for $archive"
    elif command -v shasum >/dev/null 2>&1; then
        (cd "$workdir" && printf '%s\n' "$checksum_line" | shasum -a 256 -c - >/dev/null) ||
            fail "SHA-256 verification failed for $archive"
    else
        fail "sha256sum or shasum is required to verify the download"
    fi
}

main() {
    case "$(uname -s)" in
        Linux) ;;
        Darwin) fail "on macOS, install with Homebrew: brew install ascending-llc/jarvis/jarvis-registry" ;;
        *) fail "unsupported operating system; this installer supports Linux only" ;;
    esac

    case "$(uname -m)" in
        x86_64) arch=amd64 ;;
        aarch64|arm64) arch=arm64 ;;
        *) fail "unsupported Linux architecture: $(uname -m)" ;;
    esac

    for command in curl tar mktemp awk sed install ln mv; do
        require_command "$command"
    done

    workdir="$(mktemp -d)"
    staged_binary=""
    cleanup() {
        if [[ -n "$staged_binary" ]]; then
            rm -f -- "$staged_binary"
        fi
        rm -rf -- "$workdir"
    }
    trap cleanup EXIT

    tag="$(resolve_tag)"
    version="${tag#v}"
    archive="jarvis-registry_${version}_linux_${arch}.tar.gz"
    release_url="$release_base/$tag"

    curl -fsSL "$release_url/$archive" -o "$workdir/$archive" ||
        fail "could not download $archive"
    curl -fsSL "$release_url/checksums.txt" -o "$workdir/checksums.txt" ||
        fail "could not download checksums.txt"
    verify_archive

    mkdir -p -- "$workdir/extracted"
    tar -xzf "$workdir/$archive" -C "$workdir/extracted" ||
        fail "could not extract $archive"

    for file in jarvis-registry \
        completions/jarvis-registry.bash \
        completions/jarvis-registry.zsh \
        completions/jarvis-registry.fish \
        completions/jr.fish; do
        [[ -f "$workdir/extracted/$file" ]] ||
            fail "release archive is missing $file"
    done

    bindir="${BINDIR:-$HOME/.local/bin}"
    data_home="${XDG_DATA_HOME:-$HOME/.local/share}"
    config_home="${XDG_CONFIG_HOME:-$HOME/.config}"
    bash_completion_dir="$data_home/bash-completion/completions"
    zsh_completion_dir="$data_home/zsh/site-functions"
    fish_completion_dir="$config_home/fish/completions"

    [[ ! -d "$bindir/jarvis-registry" && ! -L "$bindir/jarvis-registry" ]] ||
        fail "refusing to replace a directory or symlink at $bindir/jarvis-registry"
    [[ ! -e "$bindir/jr" || -L "$bindir/jr" ]] ||
        fail "refusing to replace an existing non-symlink at $bindir/jr"

    mkdir -p -- "$bindir" "$bash_completion_dir" "$zsh_completion_dir" "$fish_completion_dir"
    staged_binary="$(mktemp "$bindir/.jarvis-registry.XXXXXX")"
    install -m 0755 "$workdir/extracted/jarvis-registry" "$staged_binary"
    mv -f -- "$staged_binary" "$bindir/jarvis-registry"
    staged_binary=""
    ln -sfn -- jarvis-registry "$bindir/jr"

    install -m 0644 "$workdir/extracted/completions/jarvis-registry.bash" \
        "$bash_completion_dir/jarvis-registry"
    ln -sfn -- jarvis-registry "$bash_completion_dir/jr"
    install -m 0644 "$workdir/extracted/completions/jarvis-registry.zsh" \
        "$zsh_completion_dir/_jarvis-registry"
    install -m 0644 "$workdir/extracted/completions/jarvis-registry.fish" \
        "$fish_completion_dir/jarvis-registry.fish"
    install -m 0644 "$workdir/extracted/completions/jr.fish" \
        "$fish_completion_dir/jr.fish"

    case ":${PATH:-}:" in
        *":$bindir:"*) ;;
        *) printf 'Add %s to PATH if it is not already configured.\n' "$bindir" ;;
    esac

    printf -v quoted_zsh_dir '%q' "$zsh_completion_dir"
    printf 'For zsh completion, add this before compinit in ~/.zshrc:\n  fpath=(%s $fpath)\nRestart your shell afterward.\n' \
        "$quoted_zsh_dir"

    if [[ ! -f /usr/share/bash-completion/bash_completion &&
        ! -f /etc/profile.d/bash_completion.sh ]]; then
        printf 'For bash completion, install and source your distribution'\''s bash-completion package.\n'
    fi

    "$bindir/jarvis-registry" --version
}

main "$@"
