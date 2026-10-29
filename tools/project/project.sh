#!/usr/bin/env bash
# One GitHub Project (v2) shared by every practice and the contest: issues from any repo of the owner go in,
# the "Contest" field (= .env CONTEST) tells them apart. Needs `gh auth refresh -h github.com -s project`.
#
#   project.sh setup [owner] [title]   find or create the project and its fields (idempotent); prints GH_PROJECT=owner/N
#   project.sh add <issue-or-pr-url> [lane] [priority] [score]   add an item, set Status=Todo and Contest=$CONTEST,
#                                   and Lane (人|2|3|c) / Priority (P1|P2|P3) / Score (number) when given
#
# Env: GH_PROJECT (owner/N or N; N alone uses the gh user), CONTEST.
set -euo pipefail

STATUS_OPTIONS=(Todo "In Progress" Done)
PRIORITY_OPTIONS=(P1 P2 P3)
LANE_OPTIONS=(人 "2 インフラ" "3 アプリ" "c スカウト")

die() { echo "!! $*" >&2; exit 1; }
me() { gh api user --jq .login; }

owner_num() {  # GH_PROJECT -> "owner num"
  local p=${GH_PROJECT:-}
  [ -n "$p" ] || die "GH_PROJECT is empty (.env: GH_PROJECT=<owner>/<number>; task project:setup prints it)"
  case "$p" in */*) echo "${p%%/*} ${p##*/}" ;; *) echo "$(me) $p" ;; esac
}

field_json() { gh project field-list "$2" --owner "$1" --format json --limit 100; }

setup() {
  local owner=${1:-$(me)} title=${2:-ISUCON} num
  num=$(gh project list --owner "$owner" --format json --limit 100 --jq ".projects[] | select(.title == \"$title\") | .number" | head -1)
  if [ -z "$num" ]; then
    num=$(gh project create --owner "$owner" --title "$title" --format json --jq .number)
    echo "created project $owner/$num ($title)"
  else
    echo "project exists: $owner/$num ($title)"
  fi
  local fields; fields=$(field_json "$owner" "$num")
  has() { echo "$fields" | jq -e --arg n "$1" '.fields[] | select(.name == $n)' >/dev/null; }

  if ! has Lane; then
    gh project field-create "$num" --owner "$owner" --name Lane --data-type SINGLE_SELECT \
      --single-select-options "$(IFS=,; echo "${LANE_OPTIONS[*]}")" >/dev/null && echo "field Lane"
  fi
  if ! has Priority; then
    gh project field-create "$num" --owner "$owner" --name Priority --data-type SINGLE_SELECT \
      --single-select-options "$(IFS=,; echo "${PRIORITY_OPTIONS[*]}")" >/dev/null && echo "field Priority"
  fi
  has Score || { gh project field-create "$num" --owner "$owner" --name Score --data-type NUMBER >/dev/null && echo "field Score"; }
  for f in Contest Effect; do
    has "$f" || { gh project field-create "$num" --owner "$owner" --name "$f" --data-type TEXT >/dev/null && echo "field $f"; }
  done

  # Status: replace the default Todo / In Progress / Done with the team's columns (once; keeps them if already set)
  local sid cur want
  sid=$(echo "$fields" | jq -r '.fields[] | select(.name == "Status") | .id')
  cur=$(echo "$fields" | jq -r '[.fields[] | select(.name == "Status") | .options[].name] | join(",")')
  want=$(IFS=,; echo "${STATUS_OPTIONS[*]}")
  if [ -n "$sid" ] && [ "$cur" != "$want" ]; then
    local opts="" o
    for o in "${STATUS_OPTIONS[@]}"; do opts="$opts{name: \"$o\", color: GRAY, description: \"\"},"; done
    if gh api graphql -f query="mutation { updateProjectV2Field(input: {fieldId: \"$sid\", singleSelectOptions: [${opts%,}]}) { projectV2Field { ... on ProjectV2SingleSelectField { name } } } }" >/dev/null 2>&1; then
      echo "Status columns: $want"
    else
      echo "!! could not set the Status columns via the API: set them by hand in the project settings ($want)"
    fi
  fi
  echo
  echo "GH_PROJECT=$owner/$num      # put this line in .env (same project for practice and contest)"
  echo "url: https://github.com/users/$owner/projects/$num   (org owner: https://github.com/orgs/$owner/projects/$num)"
  echo "views are not creatable via the API: in the web UI add a Board view grouped by Status, filter contest:\"<CONTEST>\""
}

add() {
  local url=${1:?usage: project.sh add <url> [lane] [priority] [score]} lane=${2:-} prio=${3:-} score=${4:-} owner num pid item fields fid oid
  read -r owner num <<<"$(owner_num)"
  pid=$(gh project view "$num" --owner "$owner" --format json --jq .id)
  item=$(gh project item-add "$num" --owner "$owner" --url "$url" --format json --jq .id)
  fields=$(field_json "$owner" "$num")
  if [ -n "${CONTEST:-}" ]; then
    fid=$(echo "$fields" | jq -r '.fields[] | select(.name == "Contest") | .id')
    [ -n "$fid" ] && gh project item-edit --id "$item" --project-id "$pid" --field-id "$fid" --text "$CONTEST" >/dev/null
  fi
  fid=$(echo "$fields" | jq -r '.fields[] | select(.name == "Status") | .id')
  oid=$(echo "$fields" | jq -r '.fields[] | select(.name == "Status") | .options[] | select(.name == "Todo") | .id')
  [ -n "$fid" ] && [ -n "$oid" ] && gh project item-edit --id "$item" --project-id "$pid" --field-id "$fid" --single-select-option-id "$oid" >/dev/null
  if [ -n "$lane" ]; then
    fid=$(echo "$fields" | jq -r '.fields[] | select(.name == "Lane") | .id')
    oid=$(echo "$fields" | jq -r --arg l "$lane" '.fields[] | select(.name == "Lane") | .options[] | select(.name | startswith($l)) | .id' | head -1)
    [ -n "$fid" ] && [ -n "$oid" ] && gh project item-edit --id "$item" --project-id "$pid" --field-id "$fid" --single-select-option-id "$oid" >/dev/null
  fi
  if [ -n "$prio" ]; then
    fid=$(echo "$fields" | jq -r '.fields[] | select(.name == "Priority") | .id')
    oid=$(echo "$fields" | jq -r --arg p "$prio" '.fields[] | select(.name == "Priority") | .options[] | select(.name == $p) | .id')
    [ -n "$fid" ] && [ -n "$oid" ] && gh project item-edit --id "$item" --project-id "$pid" --field-id "$fid" --single-select-option-id "$oid" >/dev/null
  fi
  if [ -n "$score" ]; then
    fid=$(echo "$fields" | jq -r '.fields[] | select(.name == "Score") | .id')
    [ -n "$fid" ] && gh project item-edit --id "$item" --project-id "$pid" --field-id "$fid" --number "$score" >/dev/null
  fi
  echo "added to $owner/$num: $url${CONTEST:+ (Contest=$CONTEST)}${lane:+ (Lane=$lane)}${prio:+ (Priority=$prio)}${score:+ (Score=$score)}"
}

case "${1:-}" in
  setup) shift; setup "$@" ;;
  add) shift; add "$@" ;;
  *) sed -n '2,8p' "$0"; exit 2 ;;
esac
