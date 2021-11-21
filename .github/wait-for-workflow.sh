#!/usr/bin/env bash

set -euo pipefail

check_conclusion() {
  local retries count

  retries=$1
  shift

  # initial sleep in case items take a second to get onto the queue
  sleep 5

  count=0
  status="$(get_run_status)"
  until [[ $status == "completed" ]]; do
    wait=$((2 ** count))
    # wait max 15 seconds
    if (( wait > 15 )); then
      wait=15
    fi
    count=$((count + 1))
    if ((count < retries)); then
      printf "Retry $count/$retries run status=%s, checking again in $wait seconds...\n" "$status" >&2
      sleep $wait
      status="$(get_run_status)"
    else
      printf "Retry $count/$retries run status=%s, no more retries left.\n" "$status" >&2
      return 1
    fi
  done

  conclusion="$(get_run_conclusion)"
  printf "Retry $count/$retries run status=%s, conclusion=%s. Completed.\n" "$status" "$conclusion" >&2
  case $conclusion in
  success) return 0 ;;
  *) return 1 ;;
  esac
}

get_run() {
  curl -s -H "Authorization: Bearer ${GITHUB_TOKEN}" "https://api.github.com/repos/hoshsadiq/godotenv/actions/runs" |
    jq --arg head_branch "${WAIT_BRANCH}" \
      --arg github_sha "${WAIT_SHA}" \
      --arg github_event "${WAIT_EVENT}" \
      --arg workflow_name "${WAIT_WORKFLOW_NAME}" \
      '[.workflow_runs[] | select(.head_branch == $head_branch and .event == $github_event and .name == $workflow_name and .head_sha == $github_sha)] | sort_by(.created_at) | reverse | .[0]'
}

get_run_status() {
  jq <<<"$(get_run)" -r '.status'
}

get_run_conclusion() {
  jq <<<"$(get_run)" -r '.conclusion'
}

check_conclusion 300
exit $?
