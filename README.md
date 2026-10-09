# reposqueeze

`reposqueeze` — CLI и TUI на Go для переноса и синхронизации содержимого локальных Git-репозиториев с проектами на GitLab через GitLab API (без `git push` и общей истории). Умеет создавать «сиротские» ветки, отправлять и забирать отдельные файлы и коммиты, а также **двусторонне синхронизировать** локальную ветку с веткой на GitLab с 3-way merge.

## Возможности

*   **Двусторонняя синхронизация (`sync`)**: помнит, какой локальный коммит какой ветки соответствует какому коммиту на GitLab, забирает изменения с GitLab, отправляет локальные коммиты, сливает файлы, изменённые с обеих сторон.
*   **Сиротская ветка из локального репозитория** (`create-from-local`) и **из архива GitLab** (`create-from-gitlab`).
*   **Отправка** отдельных файлов (`push-files`), папки (`push-folder`), одного коммита (`cherry-pick-commit`) или изменений ветки (`push-branch`).
*   **Скачивание** файлов или изменений последних коммитов (`pull-files`).
*   **Интерактивный режим** (`reposqueeze tui`) со всеми командами.

## Установка

Нужны Go 1.25+ и Git.

```bash
git clone https://github.com/olegshirko/reposqueeze.git
cd reposqueeze
make build            # или: go build -o bin/reposqueeze ./cmd/app
export PATH=$PATH:$(pwd)/bin
```

## Настройка

Создайте **Personal Access Token** в GitLab: **Edit profile** → **Access Tokens** → **Add new token**, scope **`api`**. Скопируйте токен сразу: позже его не показать.

[СКРИНШОТ БУДЕТ ЗДЕСЬ]

| Переменная | | Описание |
|---|---|---|
| `GITLAB_TOKEN` | обязательно | Токен доступа. В логах маскируется. |
| `GITLAB_BASE_URL` | опционально | Адрес своего GitLab, например `https://gitlab.example.com` (суффикс `/api/v4` добавится сам). По умолчанию `https://gitlab.com`. |

```bash
export GITLAB_TOKEN="glpat-xxxxxxxxxxxxxxxxxxxx"
export GITLAB_BASE_URL="https://gitlab.example.com"
```

**Проект на GitLab определяется по имени папки репозитория** (`<path>`; `.` тоже работает), поиск идёт среди ваших проектов.

Формат всех команд: `reposqueeze <команда> <path> [флаги]`; флаги можно писать до или после пути. Коды выхода: `0` — успех, `1` — ошибка, `2` — неверные аргументы, `3` — sync завершился, но остались конфликты.

## Синхронизация

### Как это устроено

Через Commits API GitLab каждая отправка становится **новым коммитом с другим SHA** (и может уйти в другую ветку). Поэтому `reposqueeze` хранит **mirror**: связку «локальная ветка ↔ ветка проекта на GitLab» и журнал пар «локальный коммит ↔ коммит на GitLab с тем же содержимым».

*   Хранится в `.git/reposqueeze/mirrors.json`, то есть не попадает в коммиты.
*   `origin` — точка, с которой начали зеркалить; каждая синхронизация дописывает в журнал новую пару.
*   В сообщения коммитов (ни локальных, ни на GitLab) ничего не дописывается: соответствие коммитов хранится только в `mirrors.json`. Если файл потерян, точку синхронизации задают заново через `sync-init --local-sha ... --remote-sha ... --force`.
*   На одной ветке может быть несколько mirror'ов; тогда нужный выбирается через `--mirror <name>`.

### Команды

```bash
# 1. Один раз: зафиксировать соответствие (по умолчанию — текущие HEAD обеих веток,
#    предполагается, что содержимое сейчас совпадает)
reposqueeze sync-init . --remote-branch release

#    ...или начать с конкретных коммитов
reposqueeze sync-init . --local-branch main --local-sha a1b2c3d --remote-branch release --remote-sha 9f8e7d6

# 2. Посмотреть, что изменилось с обеих сторон
reposqueeze status .

# 3. Синхронизировать
reposqueeze sync .
reposqueeze sync . --dry-run
reposqueeze sync . --autostash --strategy merge

# 4. Журнал соответствий local <-> GitLab
reposqueeze sync-log .

# Забрать коммиты GitLab по одному (сообщение и дата сохраняются, автор — из локального git config)
reposqueeze sync . --replay --type fix --task TASK-123
```

| Флаг `sync` | Описание |
|---|---|
| `--strategy merge` | (по умолчанию) файлы, изменённые с обеих сторон, сливаются 3-way merge (`git merge-file`, база — содержимое на последней синхронизации) |
| `--strategy local` / `remote` | при конфликте побеждает локальная версия / версия GitLab |
| `--strategy abort` | при конфликтах ничего не делать, только показать список |
| `--autostash` | спрятать незакоммиченные изменения на время sync и вернуть после |
| `--dry-run` | только показать план |
| `--message` | сообщение коммита на GitLab |
| `--mirror` | имя mirror'а, если их на ветке несколько |
| `--replay` | вместо одного коммита `sync` — по локальному коммиту на каждый коммит GitLab (с исходными сообщением и датой; автор — из локального `git config`). Всё или ничего: если какой-то коммит конфликтует с локальными правками, ничего не меняется |
| `--type`, `--task` | формат локальных коммитов, см. ниже; запоминаются в mirror |

### Конфликты

Если merge не удался, в файле остаются маркеры (`<<<<<<< local` / `||||||| last sync` / `>>>>>>> gitlab`). Этот файл **не отправляется** на GitLab, остальное синхронизируется, а `sync` завершается с кодом `3`. Дальше:

```bash
# поправить файл, затем
git commit -am "resolve conflict"
reposqueeze sync .
```

Пока маркеры не убраны, повторный `sync` откажется работать. Бинарные файлы не сливаются: в рабочую копию кладётся версия с GitLab, а закоммиченной остаётся локальная. Решение принимается коммитом: следующий `sync` отправит то, что закоммичено.

### Гарантии

*   Сначала изменения с GitLab пишутся в рабочую копию, затем делается push на GitLab, и только после него — локальный коммит. Если push упал, рабочая копия возвращается в исходное состояние.
*   Если ветку на GitLab кто-то сдвинул во время sync, это обнаруживается, и новые коммиты будут подтянуты следующим `sync`.
*   Пути из архивов и diff'ов, выходящие за пределы репозитория (`../`), отклоняются.

## Перенос выбранных коммитов из GitLab

Когда нужны не все коммиты ветки GitLab, а только некоторые:

```bash
# список коммитов ветки ("*" — уже перенесён в текущую ветку)
reposqueeze pull-commit . --list --branch-name release

# перенести выбранные; порядок в списке не важен — применяются от старых к новым
reposqueeze pull-commit . --commit e797906c,8c9da15d --type fix --task TASK-123
```

Каждый коммит становится отдельным локальным коммитом с исходной датой; автор — вы, из локального `git config` (`user.name`, `user.email`). Изменение применяется как `git cherry-pick`: переносится только то, что поменял этот коммит, с 3-way merge в локальную версию файла. Уже перенесённые коммиты пропускаются (учёт ведётся в `.git/reposqueeze`, в сообщения коммитов ничего не добавляется). При конфликте перенос останавливается: в файле маркеры, сообщение коммита сохранено в `.git/reposqueeze/PICK_MSG`, и выводится готовая команда `git commit -F ... --author ...` и команда, чтобы продолжить с оставшимися коммитами.

`pull-commit` не сдвигает точку синхронизации mirror'а, а `sync --replay` не переносит повторно коммиты, взятые через `pull-commit`. В TUI это пункт **Pull commits**: выбор коммитов галочками.

## Распределение дат коммитов по периоду

При переносе (`pull-commit`, `sync --replay`) вместо исходных дат GitLab коммитам можно проставить даты, равномерно распределённые по заданному периоду:

```bash
# сначала посмотреть расписание
reposqueeze pull-commit . --commit a1b2c3d,e4f5a6b,9f8e7d6 --from 2026-09-14 --to 2026-09-25 --dry-run

# применить
reposqueeze pull-commit . --commit a1b2c3d,e4f5a6b,9f8e7d6 --from 2026-09-14 --to 2026-09-25 --type fix --task TASK-1
reposqueeze sync . --replay --from 2026-09-14 --to 2026-09-25
```

*   `--from` / `--to` — первый и последний день периода (`ГГГГ-ММ-ДД`), обязательны оба; период не может заканчиваться в будущем.
*   Коммиты делятся по рабочим дням поровну (±1 в день). Если коммитов меньше, чем дней, первый ставится на первый день, последний — на последний, остальные равномерно между ними.
*   Внутри дня — через равные интервалы в рабочих часах `--hours 10-19` (по умолчанию), со случайным сдвигом до `--jitter 20m`; порядок коммитов при этом не меняется.
*   Выходные пропускаются, если не указан `--weekends`.
*   Проставляются и дата автора, и дата коммиттера.

В TUI в Pull commits (и в Sync при переносе по коммитам) есть вопрос «Spread commit dates evenly over a period?» — после него спрашиваются даты начала и конца, рабочие часы и выходные, а перед переносом показывается расписание для подтверждения.

## Формат сообщений локальных коммитов

`--type` и `--task` (у `sync`, `sync --replay` и `pull-commit`) приводят заголовки локальных коммитов к виду `<тип>: <сообщение> <ЗАДАЧА>`:

| Коммит в GitLab | `--type fix --task TASK-1` |
|---|---|
| `feat(api): add endpoint` | `fix: add endpoint TASK-1` |
| `update docs` | `fix: update docs TASK-1` |
| служебный коммит sync | `fix: sync main with project/release TASK-1` |

Задача может содержать пробелы; в командной строке её нужно взять в кавычки: `--task "TASK 123 форма логина"`. Тело сообщения сохраняется. Если в репозитории есть hook `commit-msg`, все сообщения проверяются им **до** каких-либо изменений и до push на GitLab (нужен git 2.36+). Если hook отклонит сообщение, ничего не изменится, и будет подсказка указать `--type`/`--task`.

## Общий буфер обмена между маками (через GitLab)

Буфер переносится целиком (текст, картинки, RTF, файлы и папки) хелпером `clipsync` и шифруется общим ключом. В GitLab попадает только шифротекст и хранится одна последняя копия.

По умолчанию буфер ходит **через git по вашему SSH-ключу — токен не нужен**: приватный проект `git@gitlab.com:<вы>/clipboard.git`, ветка `clip`, в которой всегда ровно один коммит (каждая отправка делает `push --force`, история не копится). Пользователь определяется через `ssh -T` и запоминается в `~/.clipsync/remote`; проект создаётся сам при первой отправке. Если SSH недоступен — `--transport api`: Generic Package Registry проекта по `GITLAB_TOKEN`.

```bash
reposqueeze clip push            # отправить буфер этого мака
reposqueeze clip pull            # заменить буфер этого мака присланным
reposqueeze clip watch           # в фоне: отправлять, когда одно и то же скопировано дважды (⌘C ⌘C)
reposqueeze clip watch --pull    # ...и самому принимать копии с другого мака (проверка раз в 5 с)
```

Обычное одиночное копирование никуда не уходит — отправка только по двойному ⌘C (окно `--window 1s`). Свои же отправки при автоприёме не принимаются. В TUI — пункты «Clipboard → GitLab» и «Clipboard ← GitLab».

### Установка (на каждом маке)

1. Собрать reposqueeze и положить в `~/bin`: `make build && cp bin/reposqueeze ~/bin/`.
2. Собрать хелпер: `make clipsync` (нужны Xcode Command Line Tools). Он ставится в `~/.clipsync/bin/clipsync`; прежние команды `export`/`import` не изменились, добавился режим `watch`.
3. Скопировать **один и тот же** ключ `~/.clipsync/key` на оба мака — не через GitLab.
4. Проверить SSH: `ssh -T git@gitlab.com` должен ответить «Welcome to GitLab». Если на работе закрыт порт 22, в `~/.ssh/config` для `gitlab.com` укажите `Hostname altssh.gitlab.com` и `Port 443`.
5. Фоновое наблюдение при входе в систему — LaunchAgent из `tools/launchd/` (инструкция в файле). Для автоприёма допишите в нём флаг `--pull`.
6. Горячие клавиши Raycast — скрипты из `tools/raycast/`: отправить, забрать, забрать и сразу вставить (удобно повесить на ⌥⌘V). Если GitLab свой, раскомментируйте в них `GITLAB_BASE_URL`.

Ключ, проект и адрес можно поменять: `--key` / `REPOSQUEEZE_CLIP_KEY`, `--project`, `--remote`; путь к хелперу — `REPOSQUEEZE_CLIPSYNC`. Для `--transport api` в проекте должен быть включён Package Registry.

## Остальные команды

```bash
# Сиротская ветка из локальной ветки; проект на GitLab ПЕРЕСОЗДАЁТСЯ
reposqueeze create-from-local . --branch-name gh-pages --from main

# Архив проекта GitLab -> локальная ветка (существующая или новая сиротская)
reposqueeze create-from-gitlab . --branch-name from-gitlab [--ref <branch|tag|sha>] [--commit]

# Отправить файлы в существующую ветку
reposqueeze push-files . --branch-name main --files README.md,src/main.go

# Загрузить папку в проект; проект ПЕРЕСОЗДАЁТСЯ (.git, vendor, node_modules пропускаются)
reposqueeze push-folder ./dist --project-name site --branch-name main

# Отправить изменения одного локального коммита
reposqueeze cherry-pick-commit . --commit a1b2c3d --branch-name main [--message "..."]

# Отправить изменения ветки относительно master/main одним коммитом (vendor исключается)
reposqueeze push-branch . --source-branch feature/x --branch-name main [--message "..."]

# ...в новую ветку GitLab: сначала создать её от существующей (проект не трогается)
reposqueeze push-branch . --source-branch feature/x --branch-name feature/x --create-from master

# Скачать файлы с GitLab
reposqueeze pull-files . --branch-name main --files README.md,go.mod   # конкретные файлы
reposqueeze pull-files . --branch-name main --commits 3 --git-add      # изменения 3 последних коммитов
reposqueeze pull-files . --branch-name main --since-commit 9f8e7d6     # всё начиная с коммита
```

Ветку, созданную `create-from-gitlab`, можно влить в рабочую:

```bash
git checkout <ваша ветка>
git merge from-gitlab --allow-unrelated-histories
```

## Интерактивный режим

```bash
reposqueeze tui
```

Меню со всеми командами. В Push branch в списке целевых веток есть «+ new branch…» — имя новой ветки и ветка GitLab, от которой её создать. В Sync можно включить перенос по коммитам и задать тип/задачу; в Pull commits выбрать нужные коммиты из списка. Для sync сначала показывается план (что будет забрано, отправлено и слито), затем запрос подтверждения. В Sync init локальный коммит и соответствующий ему коммит GitLab выбираются из списков. `esc` отменяет текущую операцию и возвращает в меню.

## Структура проекта

```
cmd/app/main.go                     точка входа: CLI и TUI
internal/
  app/controller/                   разбор аргументов CLI, коды выхода
  app/usecase/                      сценарии: sync*.go, push_*.go, pull_files.go, create_*.go
  app/tui/                          Bubble Tea интерфейс
  domain/entity/                    Mirror, SyncPoint, Project...
  domain/gateway/                   интерфейсы GitGateway, SyncGit, GitLabGateway, MirrorStore
  infrastructure/git/               git через os/exec
  infrastructure/gitlab/            клиент GitLab REST API
  infrastructure/state/             хранение mirror'ов в .git/reposqueeze
  pkg/logger/                       логгер с маскировкой токена
```

## Разработка

```bash
go test ./...
go vet ./...
```

Тесты sync работают с настоящим git-репозиторием и GitLab, эмулируемым в памяти (в том числе через HTTP-клиент), поэтому сеть и токен для них не нужны.
