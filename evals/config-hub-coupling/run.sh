#!/usr/bin/env bash
# Runs the config-hub-coupling eval: N fresh agents per arm, then grades each.
#
# Usage: ./run.sh [--arm with|without] [--reps N] [--out DIR]
#
#   --arm without   no guidance (baseline: shows the failure exists)
#   --arm with      SKILL.md + references/implementation.md in the prompt
#
# Each agent gets an identical copy of fixture/ and the task in task.md, and
# runs headless with no access to this repo. grade.sh scores the result.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
skill_dir=$(cd "$here/../../skills/loading-configuration" && pwd)

arm=with
reps=5
out=$here/results/$(date +%Y%m%d-%H%M%S)

while [ $# -gt 0 ]; do
  case $1 in
    --arm)  arm=$2; shift 2 ;;
    --reps) reps=$2; shift 2 ;;
    --out)  out=$2; shift 2 ;;
    *) echo "unknown flag: $1" >&2; exit 2 ;;
  esac
done

mkdir -p "$out"
pass=0

for rep in $(seq 1 "$reps"); do
  d=$out/$arm-$rep
  mkdir -p "$d"
  cp -r "$here/fixture" "$d/work"

  {
    echo "You are working in: $d/work"
    echo
    if [ "$arm" = with ]; then
      echo "## Project guidance (follow it)"
      echo
      echo "The team has a skill covering this. Its SKILL.md:"
      echo
      echo '<SKILL.md>'
      cat "$skill_dir/SKILL.md"
      echo '</SKILL.md>'
      echo
      echo "The reference it points to, references/implementation.md:"
      echo
      echo '<implementation.md>'
      cat "$skill_dir/references/implementation.md"
      echo '</implementation.md>'
      echo
    fi
    cat "$here/task.md"
  } > "$d/PROMPT.md"

  claude -p "$(cat "$d/PROMPT.md")" \
    --model sonnet \
    --allowedTools Read Write Edit Bash Glob Grep \
    > "$d/reply.txt" 2>"$d/stderr.txt" || true

  echo "--- $arm-$rep ---"
  if "$here/grade.sh" "$d/work" | tee "$d/grade.txt"; then
    pass=$((pass + 1))
  fi
done

echo
echo "$arm arm: $pass/$reps PASS"
echo "results: $out"
[ "$pass" = "$reps" ]
