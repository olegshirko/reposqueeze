#!/bin/bash
# @raycast.schemaVersion 1
# @raycast.title Clip ← GitLab и вставить
# @raycast.mode silent
# @raycast.icon 📋
# @raycast.packageName Clipboard Sync
# @raycast.description Забрать буфер с другого мака и сразу вставить (назначьте, например, ⌥⌘V)

GITLAB_TOKEN=$(/usr/bin/security find-generic-password -a "$USER" -s reposqueeze-gitlab -w 2>/dev/null) \
  || { echo "нет токена в связке ключей (reposqueeze-gitlab)"; exit 1; }
export GITLAB_TOKEN
# export GITLAB_BASE_URL=https://gitlab.example.com

if "$HOME/bin/reposqueeze" clip pull --quiet >/dev/null 2>&1; then
  # ⌘V в активное приложение: Raycast должен иметь доступ «Универсальный доступ»
  /usr/bin/osascript -e 'tell application "System Events" to keystroke "v" using command down'
else
  echo "не удалось забрать буфер"
  exit 1
fi
