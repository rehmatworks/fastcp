#!/usr/bin/env bash
# Add bash shebang to files that look like shell scripts but are missing one.
# Usage: ./scripts/add_shebangs.sh [paths...]

set -euo pipefail

DRY_RUN=0
paths=("${@:-.}")

# simple CLI parsing for --dry-run
for arg in "$@"; do
  if [ "$arg" = "--dry-run" ] || [ "$arg" = "-n" ]; then
    DRY_RUN=1
  fi
done

find_files() {
  for p in "${paths[@]}"; do
    # common script names and .sh files
    find "$p" -type f \( -name "*.sh" -o -name "entrypoint*" -o -name "*.zsh" \) \
      -not -path "*/.venv/*" -not -path "*/node_modules/*" -print
  done
}

for f in $(find_files | sort -u); do
  # skip binary files
  if file --mime-encoding "$f" | grep -q binary; then
    continue
  fi
  first_line=$(head -n1 "$f" || true)
  if [[ "$first_line" =~ ^#! ]]; then
    echo "OK: $f (has shebang)"
    continue
  fi
  echo "Patching: $f"
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "(dry-run) would prepend shebang to $f"
    continue
  fi
  cp "$f" "$f.bak"
  tmp=$(mktemp)
  echo '#!/usr/bin/env bash' > "$tmp"
  echo '# shellcheck shell=bash' >> "$tmp"
  cat "$f" >> "$tmp"
  mv "$tmp" "$f"
  chmod +x "$f"
done

echo "Done. Backups are saved with .bak suffix. Review changes before committing."
