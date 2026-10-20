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

holder="" age=$STALE_SECS lease_note="lease-missing"
if [[ -f $STATE/lease.json ]]; then
  if holder=$(jq -er '.holder' "$STATE/lease.json" 2>/dev/null) \
     && ts=$(jq -er '.renewed_at | sub("\\.[0-9]+"; "") | sub("\\+00:00$"; "Z") | fromdateiso8601' "$STATE/lease.json" 2>/dev/null); then
    age=$(( $(date -u +%s) - ${ts%.*} ))
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
