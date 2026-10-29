#!/bin/sh
# Colour bootstrap / setup output: NG red, OK green, ## bold, task: Failed red. Usage: ... | sh tools/bootstrap/colorize.sh
exec awk '
/^NG/ || /^task: Failed/ || /^!!/ { printf "\033[1;31m%s\033[0m\n", $0; next }
/^OK/ || /^authorized$/           { printf "\033[32m%s\033[0m\n", $0; next }
/^##/ || /^---/ || /^\[[0-9]+s\]/  { printf "\033[1m%s\033[0m\n", $0; next }
{ print }'
