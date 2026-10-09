#!/bin/bash
# @raycast.schemaVersion 1
# @raycast.title Clip ← GitLab
# @raycast.mode silent
# @raycast.icon 📥
# @raycast.packageName Clipboard Sync
# @raycast.description Забрать буфер обмена с другого мака через GitLab

GITLAB_TOKEN=$(/usr/bin/security find-generic-password -a "$USER" -s reposqueeze-gitlab -w 2>/dev/null) \
  || { echo "нет токена в связке ключей (reposqueeze-gitlab)"; exit 1; }
export GITLAB_TOKEN
# export GITLAB_BASE_URL=https://gitlab.example.com

out=$("$HOME/bin/reposqueeze" clip pull --quiet 2>&1 | tail -1)
echo "$out"
