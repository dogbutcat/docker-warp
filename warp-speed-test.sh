#!/bin/bash
set -uo pipefail

WARP_IP_SELECTION_ENABLED="${WARP_IP_SELECTION_ENABLED:-false}"
WARP_API_SELECTION_ENABLED="${WARP_API_SELECTION_ENABLED:-false}"
WARP_IPV6_SELECTION="${WARP_IPV6_SELECTION:-false}"
WARP_TUNNEL_PROTOCOL="${WARP_TUNNEL_PROTOCOL:-masque}"
WARP_MDM_ENABLED="${WARP_MDM_ENABLED:-false}"
WARP_LOG_LEVEL="${WARP_LOG_LEVEL:-info}"
WARP_OVERRIDE_WARP_ENDPOINT="${WARP_OVERRIDE_WARP_ENDPOINT:-}"
WARP_OVERRIDE_API_ENDPOINT="${WARP_OVERRIDE_API_ENDPOINT:-}"

CACHE_DIR="/var/lib/cloudflare-warp"
TUNNEL_CACHE_FILE="${CACHE_DIR}/warp-best-endpoint.json"
API_CACHE_FILE="${CACHE_DIR}/warp-best-api-endpoint.json"
CACHE_TTL=259200
LOG_FILE="${CACHE_DIR}/warp-speed-test.log"
PROBE_BIN="/usr/local/bin/warp-endpoint-probe"
TOTAL_TIMEOUT="3s"
PROBE_CONCURRENCY="${WARP_PROBE_CONCURRENCY:-200}"

mkdir -p "$CACHE_DIR"

log_level_value() {
  case "$1" in
    debug) echo 0 ;;
    info) echo 1 ;;
    warn) echo 2 ;;
    error) echo 3 ;;
    *) echo 1 ;;
  esac
}

CURRENT_LOG_LEVEL=$(log_level_value "$WARP_LOG_LEVEL")

log() {
  local level="$1"
  shift
  local level_value
  level_value=$(log_level_value "$level")
  if [ "$level_value" -lt "$CURRENT_LOG_LEVEL" ]; then
    return
  fi
  local timestamp
  timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
  local message
  message="{\"time\":\"${timestamp}\",\"level\":\"${level}\",\"msg\":\"$*\"}"
  echo "$message" >> "$LOG_FILE"
  echo "$message" >&2
}

log_debug() { log debug "$@"; }
log_info() { log info "$@"; }
log_warn() { log warn "$@"; }
log_error() { log error "$@"; }

cache_get_endpoint() {
  local cache_file="$1"

  if [ ! -f "$cache_file" ] || [ ! -s "$cache_file" ]; then
    return 1
  fi

  local timestamp
  local ttl
  local endpoint
  local now_epoch
  local cache_epoch

  timestamp=$(jq -r '.timestamp // empty' "$cache_file" 2>/dev/null) || return 1
  ttl=$(jq -r '.ttl // 259200' "$cache_file" 2>/dev/null) || return 1
  endpoint=$(jq -r '.endpoint // empty' "$cache_file" 2>/dev/null) || return 1

  if [ -z "$timestamp" ] || [ -z "$endpoint" ]; then
    return 1
  fi

  now_epoch=$(date +%s)
  cache_epoch=$(date -d "$timestamp" +%s 2>/dev/null) || return 1

  if [ $((now_epoch - cache_epoch)) -lt "$ttl" ]; then
    echo "$endpoint"
    return 0
  fi

  return 1
}

cache_write_tunnel() {
  local endpoint="$1"
  local latency_ms="$2"
  local target="$3"
  local timestamp
  timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

  cat > "$TUNNEL_CACHE_FILE" <<EOF
{
  "endpoint": "${endpoint}",
  "timestamp": "${timestamp}",
  "ttl": ${CACHE_TTL},
  "metadata": {
    "mode": "tunnel",
    "target": "${target}",
    "protocol": "${WARP_TUNNEL_PROTOCOL}",
    "latency_ms": ${latency_ms:-0},
    "ip_pool": "${target}"
  }
}
EOF
  chmod 664 "$TUNNEL_CACHE_FILE"
}

cache_write_api() {
  local endpoint="$1"
  local latency_ms="$2"
  local timestamp
  timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

  cat > "$API_CACHE_FILE" <<EOF
{
  "endpoint": "${endpoint}",
  "timestamp": "${timestamp}",
  "ttl": ${CACHE_TTL},
  "metadata": {
    "mode": "api",
    "target": "jdcloud",
    "latency_ms": ${latency_ms:-0},
    "ip_pool": "jdcloud"
  }
}
EOF
  chmod 664 "$API_CACHE_FILE"
}

infer_tunnel_target() {
  if [ "$WARP_MDM_ENABLED" != "true" ]; then
    echo "consumer"
    return
  fi
  if [ "$WARP_TUNNEL_PROTOCOL" = "masque" ]; then
    echo "masque"
    return
  fi
  echo "wireguard"
}

run_probe() {
  local mode="$1"
  local target="${2:-}"
  local csv_file
  csv_file=$(mktemp /tmp/warp-probe.XXXXXX.csv)

  local command=("$PROBE_BIN" "-mode" "$mode" "-n" "$PROBE_CONCURRENCY" "-timeout" "$TOTAL_TIMEOUT" "-o" "$csv_file")
  if [ -n "$target" ]; then
    command+=("-target" "$target")
  fi
  if [ "$WARP_IPV6_SELECTION" = "true" ]; then
    command+=("-6")
  fi

  if ! "${command[@]}" >> "$LOG_FILE" 2>&1; then
    rm -f "$csv_file"
    return 1
  fi

  if [ ! -s "$csv_file" ]; then
    rm -f "$csv_file"
    return 1
  fi

  local best_line
  best_line=$(head -1 "$csv_file")
  rm -f "$csv_file"
  if [ -z "$best_line" ]; then
    return 1
  fi

  local endpoint
  local latency
  endpoint=$(echo "$best_line" | cut -d',' -f1)
  latency=$(echo "$best_line" | cut -d',' -f2)
  if [ -z "$endpoint" ]; then
    return 1
  fi

  echo "${endpoint},${latency:-0}"
  return 0
}

select_tunnel_endpoint() {
  if [ "$WARP_IP_SELECTION_ENABLED" != "true" ]; then
    log_info "Tunnel selection disabled"
    return 0
  fi

  if [ -n "$WARP_OVERRIDE_WARP_ENDPOINT" ]; then
    log_info "Manual tunnel endpoint already configured: ${WARP_OVERRIDE_WARP_ENDPOINT}"
    echo "$WARP_OVERRIDE_WARP_ENDPOINT"
    return 0
  fi

  local cached
  if cached=$(cache_get_endpoint "$TUNNEL_CACHE_FILE"); then
    log_info "Tunnel cache hit: ${cached}"
    echo "$cached"
    return 0
  fi

  local target
  target=$(infer_tunnel_target)
  local probe_output
  if ! probe_output=$(run_probe tunnel "$target"); then
    log_warn "Tunnel probe failed, soft-fail"
    return 0
  fi

  local endpoint
  local latency
  endpoint=$(echo "$probe_output" | cut -d',' -f1)
  latency=$(echo "$probe_output" | cut -d',' -f2)
  cache_write_tunnel "$endpoint" "$latency" "$target"
  log_info "Tunnel endpoint selected: ${endpoint} (${latency}ms, target=${target})"
  echo "$endpoint"
}

select_api_endpoint() {
  if [ "$WARP_API_SELECTION_ENABLED" != "true" ]; then
    log_info "API selection disabled"
    return 0
  fi

  if [ -n "$WARP_OVERRIDE_API_ENDPOINT" ]; then
    log_info "Manual API endpoint already configured: ${WARP_OVERRIDE_API_ENDPOINT}"
    echo "$WARP_OVERRIDE_API_ENDPOINT"
    return 0
  fi

  local cached
  if cached=$(cache_get_endpoint "$API_CACHE_FILE"); then
    log_info "API cache hit: ${cached}"
    echo "$cached"
    return 0
  fi

  local probe_output
  if ! probe_output=$(run_probe api); then
    log_warn "API probe failed, soft-fail"
    return 0
  fi

  local endpoint
  local latency
  endpoint=$(echo "$probe_output" | cut -d',' -f1)
  latency=$(echo "$probe_output" | cut -d',' -f2)
  cache_write_api "$endpoint" "$latency"
  log_info "API endpoint selected: ${endpoint} (${latency}ms)"
  echo "$endpoint"
}

if [ ! -x "$PROBE_BIN" ]; then
  log_error "Probe binary not found: ${PROBE_BIN}"
  exit 0
fi

case "${1:-}" in
  --tunnel)
    select_tunnel_endpoint || true
    ;;
  --api)
    select_api_endpoint || true
    ;;
  "")
    tunnel_endpoint="$(select_tunnel_endpoint || true)"
    api_endpoint="$(select_api_endpoint || true)"
    [ -n "$tunnel_endpoint" ] && echo "TUNNEL_ENDPOINT=${tunnel_endpoint}"
    [ -n "$api_endpoint" ] && echo "API_ENDPOINT=${api_endpoint}"
    ;;
  *)
    log_error "Unsupported argument: $1"
    exit 0
    ;;
esac
