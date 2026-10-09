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
*   Каждый sync создаёт локальный коммит с трейлером `Reposqueeze-Remote: <project>/<branch>@<sha>`, а коммиты на GitLab получают `Reposqueeze-Source: <branch>@<sha>`. По этим трейлерам mirror восстанавливается, если файл потерян (`sync-init --recover`).
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

# Забрать коммиты GitLab по одному (сообщение, автор и дата сохраняются)
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
| `--replay` | вместо одного коммита `sync` — по локальному коммиту на каждый коммит GitLab (с исходными сообщением, автором и датой). Всё или ничего: если какой-то коммит конфликтует с локальными правками, ничего не меняется |
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

Каждый коммит становится отдельным локальным коммитом с исходными автором и датой и трейлером `Reposqueeze-Replayed-From`. Изменение применяется как `git cherry-pick`: переносится только то, что поменял этот коммит, с 3-way merge в локальную версию файла. Уже перенесённые коммиты пропускаются. При конфликте перенос останавливается: в файле маркеры, сообщение коммита сохранено в `.git/reposqueeze/PICK_MSG`, и выводится готовая команда `git commit -F ... --author ...` и команда, чтобы продолжить с оставшимися коммитами.

`pull-commit` не сдвигает точку синхронизации mirror'а, а `sync --replay` не переносит повторно коммиты, взятые через `pull-commit`. В TUI это пункт **Pull commits**: выбор коммитов галочками.

## Формат сообщений локальных коммитов

`--type` и `--task` (у `sync`, `sync --replay` и `pull-commit`) приводят заголовки локальных коммитов к виду `<тип>: <сообщение> <ЗАДАЧА>`:

| Коммит в GitLab | `--type fix --task TASK-1` |
|---|---|
| `feat(api): add endpoint` | `fix: add endpoint TASK-1` |
| `update docs` | `fix: update docs TASK-1` |
| служебный коммит sync | `fix: sync main with project/release TASK-1` |

Тело сообщения сохраняется. Если в репозитории есть hook `commit-msg`, все сообщения проверяются им **до** каких-либо изменений и до push на GitLab (нужен git 2.36+). Если hook отклонит сообщение, ничего не изменится, и будет подсказка указать `--type`/`--task`.

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

Меню со всеми командами. В Sync можно включить перенос по коммитам и задать тип/задачу; в Pull commits выбрать нужные коммиты из списка. Для sync сначала показывается план (что будет забрано, отправлено и слито), затем запрос подтверждения. В Sync init локальный коммит и соответствующий ему коммит GitLab выбираются из списков. `esc` отменяет текущую операцию и возвращает в меню.

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
