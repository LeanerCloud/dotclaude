#!/usr/bin/env bash
# Fleet-run heartbeat (no LLM). Run every 20 min by launchd (macOS) or a systemd user timer (Linux).
# Wakes the lease-holding coordinator; when the lease is stale and the holder does not respond, wakes the
# next standby to take the lease. Never starts, kills or answers dialogs of any agent.
# Expects state/lease.json = {"holder": "<agent name>", "renewed_at": "<ISO-8601 UTC>"}, renewed by the holder.
# Usage: heartbeat.sh [coordinator names in failover order]
set -euo pipefail
export PATH="$PATH:/opt/homebrew/bin:/usr/local/bin:$HOME/.local/bin"

STATE="$HOME/.claude/fleet-run/state"
LOG="$STATE/heartbeat.log"
STRIKES="$STATE/heartbeat.strikes"
STALE_SECS=$((45 * 60))
ESCALATE_AFTER=3
COORDS=("$@")
(( ${#COORDS[@]} )) || COORDS=(coord-claude coord-kimi coord-opencode)

mkdir -p "$STATE"
log() { printf '%s\t%s\n' "$(date -u +%FT%TZ)" "$*" >>"$LOG"; }

# Count consecutive bad outcomes; after ESCALATE_AFTER, record once in needs-user.md.
strikes() { local n; n=$(cat "$STRIKES" 2>/dev/null || true); [[ $n =~ ^[0-9]+$ ]] && echo "$n" || echo 0; }
strike() {
  local n; n=$(( $(strikes) + 1 ))
  echo "$n" >"$STRIKES"
  if (( n == ESCALATE_AFTER )); then
    printf -- '- %s heartbeat: %s (%d consecutive runs)\n' "$(date -u +%FT%TZ)" "$1" "$n" >>"$STATE/needs-user.md"
  fi
}
clear_strikes() { rm -f "$STRIKES"; }

for dep in herdr jq; do
  command -v "$dep" >/dev/null || { log "missing-dep $dep"; strike "missing dependency $dep"; exit 1; }
done
if ! herdr agent list >/dev/null 2>&1; then
  log "herdr-unreachable"; strike "herdr unreachable"; exit 1
fi

status_of() {
  local s; s=$(herdr agent get "$1" 2>/dev/null | jq -r '.result.agent.agent_status // empty' 2>/dev/null) || s=""
  echo "${s:-missing}"
}
wake() { herdr agent prompt "$1" "$2" >/dev/null 2>&1 && log "woke $1: $2" || { log "wake-failed $1"; return 1; }; }

# ISO-8601 with Z or +HH:MM/-HH:MM offset and optional fraction -> UTC epoch seconds. Pure arithmetic
# (days-from-civil) because BSD and GNU date disagree on parsing. Returns 1 on anything else.
iso_epoch() {
  local re='^([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})(\.[0-9]+)?(Z|([+-])([0-9]{2}):([0-9]{2}))$'
  [[ $1 =~ $re ]] || return 1
  local y=$((10#${BASH_REMATCH[1]})) m=$((10#${BASH_REMATCH[2]})) d=$((10#${BASH_REMATCH[3]}))
  local hh=$((10#${BASH_REMATCH[4]})) mi=$((10#${BASH_REMATCH[5]})) ss=$((10#${BASH_REMATCH[6]}))
  (( m >= 1 && m <= 12 && d >= 1 && hh < 24 && mi < 60 && ss <= 60 )) || return 1
  local dim=(0 31 28 31 30 31 30 31 31 30 31 30 31)
  (( m == 2 && (y % 4 == 0 && (y % 100 != 0 || y % 400 == 0)) )) && dim[2]=29
  (( d <= dim[m] )) || return 1
  local off=0
  if [[ ${BASH_REMATCH[8]} != Z ]]; then
    (( 10#${BASH_REMATCH[10]} <= 14 && 10#${BASH_REMATCH[11]} <= 59 )) || return 1
    off=$(( 10#${BASH_REMATCH[10]} * 3600 + 10#${BASH_REMATCH[11]} * 60 ))
    [[ ${BASH_REMATCH[9]} == - ]] && off=$(( -off ))
  fi
  (( m <= 2 )) && y=$((y - 1))
  local era=$(( y / 400 )) yoe doy
  yoe=$(( y - era * 400 ))
  doy=$(( (153 * (m > 2 ? m - 3 : m + 9) + 2) / 5 + d - 1 ))
  echo $(( (era * 146097 + yoe * 365 + yoe / 4 - yoe / 100 + doy - 719468) * 86400 + hh * 3600 + mi * 60 + ss - off ))
}

holder="" age=$STALE_SECS lease_note="lease-missing"
if [[ -f $STATE/lease.json ]]; then
  if holder=$(jq -er '.holder' "$STATE/lease.json" 2>/dev/null) \
     && raw=$(jq -er '.renewed_at' "$STATE/lease.json" 2>/dev/null) && ts=$(iso_epoch "$raw"); then
    age=$(( $(date -u +%s) - ts ))
    lease_note="lease_age=${age}s"
    if (( age < -300 )); then lease_note="lease-in-future(${age}s)"; age=$STALE_SECS; fi
  else
    holder="" lease_note="lease-corrupt"
  fi
fi
if [[ -n $holder && ! $holder =~ ^[a-z][a-z0-9_-]{0,31}$ ]]; then
  holder="" age=$STALE_SECS lease_note="holder-invalid"
fi

if [[ -n $holder ]]; then
  st=$(status_of "$holder")
  if [[ $st == working ]]; then
    log "ok $holder working $lease_note"
    (( age < STALE_SECS )) && clear_strikes || strike "$holder working but lease not renewed ($lease_note)"
    exit 0
  fi
  if (( age < STALE_SECS )); then
    case $st in
      idle|done)
        if wake "$holder" "tick"; then clear_strikes; else strike "tick to $holder failed (fresh lease)"; fi
        exit 0 ;;
      *) log "holder $holder status=$st $lease_note; waiting"; strike "$holder is $st"; exit 0 ;;
    esac
  fi
fi

# Stale or no lease. If any coordinator is mid-takeover, let it finish.
for c in "${COORDS[@]}"; do
  if [[ $c != "$holder" && $(status_of "$c") == working ]]; then
    log "takeover-in-progress by $c holder=${holder:-none} $lease_note"
    strike "takeover by $c not completing ($lease_note)"
    exit 0
  fi
done
# The stale holder gets the first chances; after ESCALATE_AFTER stale runs it is skipped so standbys get a
# turn. Strikes are cleared only when a fresh lease is observed, never by a takeover wake.
order=()
(( $(strikes) < ESCALATE_AFTER )) && [[ -n $holder ]] && order+=("$holder")
for c in "${COORDS[@]}"; do [[ $c == "$holder" ]] || order+=("$c"); done
(( ${#order[@]} )) || order=("${COORDS[@]}")
for c in "${order[@]}"; do
  st=$(status_of "$c")
  [[ $st == idle || $st == "done" ]] || continue
  if wake "$c" "takeover: lease holder '${holder:-none}' ($lease_note); follow the coordinator prompt to acquire or renew it"; then
    strike "lease stale ($lease_note); woke $c"
    exit 0
  fi
done
log "no-ready-coordinator holder=${holder:-none} $lease_note"
strike "no coordinator ready to take the lease"
exit 1
