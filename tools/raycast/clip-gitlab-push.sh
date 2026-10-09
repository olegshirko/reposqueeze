#!/bin/bash
# @raycast.schemaVersion 1
# @raycast.title Clip → GitLab
# @raycast.mode silent
# @raycast.icon 📤
# @raycast.packageName Clipboard Sync
# @raycast.description Отправить буфер обмена на другой мак через GitLab

# Буфер ходит через git по SSH-ключу (токен не нужен). Свой GitLab:
# export GITLAB_BASE_URL=https://gitlab.example.com

out=$("$HOME/bin/reposqueeze" clip push --quiet 2>&1 | tail -1)
echo "$out"
