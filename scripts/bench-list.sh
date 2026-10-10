#!/usr/bin/env bash
# Measure end-to-end wall time of `shine list`.
#
# Usage:
#   scripts/bench-list.sh [runs]
#
# Environment:
#   BIN      path to the executable      (default: ./build/shine)
#   WARMUP   untimed runs before measuring (default: 3)
#   LABEL    name printed in the summary  (default: empty)
#
# Example:
#   LABEL=parallel scripts/bench-list.sh 50

set -euo pipefail

BIN="${BIN:-./build/shine}"
RUNS="${1:-30}"
WARMUP="${WARMUP:-3}"
LABEL="${LABEL:-}"

if [[ ! -x "$BIN" ]]; then
	echo "error: $BIN is not an executable file" >&2
	exit 1
fi

# Microseconds since epoch, from bash's EPOCHREALTIME (e.g. "1728600000.123456").
now_us() {
	local t=$EPOCHREALTIME
	t=${t/./}
	echo $((10#$t))
}

for ((i = 0; i < WARMUP; i++)); do
	"$BIN" list >/dev/null 2>&1 || true
done

times=()
failures=0
for ((i = 0; i < RUNS; i++)); do
	start=$(now_us)
	if ! "$BIN" list >/dev/null 2>&1; then
		failures=$((failures + 1))
	fi
	end=$(now_us)
	times+=("$((end - start))")
done

printf '%s\n' "${times[@]}" | sort -n | awk \
	-v label="$LABEL" -v runs="$RUNS" -v failures="$failures" '
{
	v[NR] = $1 / 1000.0   # microseconds -> milliseconds
	sum += v[NR]
}
END {
	if (NR == 0) exit 1
	p = int((NR - 1) * 0.9) + 1
	median = (NR % 2) ? v[(NR + 1) / 2] : (v[NR / 2] + v[NR / 2 + 1]) / 2
	header = (label == "") ? "shine list" : "shine list [" label "]"
	printf "%s, %d runs (%d failed)\n", header, runs, failures
	printf "  min:    %8.2f ms\n", v[1]
	printf "  median: %8.2f ms\n", median
	printf "  mean:   %8.2f ms\n", sum / NR
	printf "  p90:    %8.2f ms\n", v[p]
	printf "  max:    %8.2f ms\n", v[NR]
}'
