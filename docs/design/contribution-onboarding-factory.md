# Contribution Onboarding: ежедневный дренаж очереди контрибьюшенов в PR

Дата: 2026-08-19
Статус: дизайн утверждён, готов к планированию

## Проблема

В freehire (`../hire`) поток «предложи борд» пишет в таблицу `link_contributions` строки
со статусом `pending` и никогда ничего не краулит — миграция `0025` прямо называет статус
«швом для отложенного ingest-воркера». Этого воркера нет; вместо него есть локальный скилл
`onboard-contributions`, который человек запускает руками: читает очередь на проде, живьём
проверяет борд, дописывает `sources/<source>.yml`, открывает PR и после мержа флипает статус.

Пока очередь не дренится, борд не краулится, а повторный контрибьют того же борда снова
начисляет очки — награда перестаёт быть честной.

## Решение

Bundled-фабрика SuperPlane `contribution-onboarding`: ежедневный canvas, где один узел
**Run Claude Code** проходит всю процедуру и заканчивается открытым PR. Мержит человек.

Фабрика, а не разовый canvas: в репозитории уже есть механизм bundled-шаблонов
(`web_src/src/pages/home/factories/`), и шаблон ставится в организацию рядом с существующим
digest-канвасом через мастер установки с `install_params`. Специфика freehire (хост прода,
имя БД, репозиторий) уходит в параметры установки, а не в тело canvas.

### Артефакты

```
web_src/src/pages/home/factories/contribution-onboarding/
  canvas.yaml     — граф: Schedule + Run Now → Run Claude Code → уведомление
  console.yaml    — панель: runbook, ссылка на последний PR, состояние очереди
  factory.json    — id, title, integrations, componentIntegrations, run, startingTasks
  params.json     — install_params
web_src/src/pages/home/factories/index.ts   — регистрация в FACTORY_BY_ID
```

## Топология canvas

```
[Schedule · cron]        [Run Now · onRun]
        \                      /
         \                    /
      [Run Claude Code · Onboard contributions]
                    |
        passed      |      failed
          ↓                 ↓
  [Telegram · итог]   [Telegram · падение]
```

Один узел вместо цепочки: prompt-шаги внутри `runnerClaudeCode` продолжают **одну сессию**
(`run.js` добавляет `--continue` начиная со второго промпта), поэтому агент, провалидировавший
борд, помнит это на шаге написания PR-описания. Разрезать на узлы — потерять контекст и
передавать состояние через выражения.

`onRun` рядом с `Schedule` даёт кнопку «прогнать сейчас» и закрывает обязательное поле
`run` в `factory.json`.

## Конфигурация узла Run Claude Code

```yaml
component: runnerClaudeCode
configuration:
  machineType: '{{ install_params.machineType }}'
  executionTimeoutSeconds: 7200
  credentials:
    source: integration
    integration:
      name: claude
  environmentFrom:
    - source: integration
      integration:
        name: github
  environment:
    - name: REPO
      value: '{{ install_params.repository }}'
      valueSource: literal
    - name: PROD_SSH_HOST
      value: '{{ install_params.prodSshHost }}'
      valueSource: literal
    - name: PROD_DB
      value: '{{ install_params.prodDbName }}'
      valueSource: literal
    - name: PROD_SSH_KEY
      valueSource: secret
      # ссылка на секрет из install_params.sshSecret
```

`credentials.source` обязан быть `integration`. При `source: secret` компонент кладёт значение
в `ANTHROPIC_API_KEY` жёстко (`pkg/components/runner/claude/component.go:410`), а токен
подписки (`sk-ant-oat…`) должен приехать как `CLAUDE_CODE_OAUTH_TOKEN` — иначе `run.js:56`
оставит `--bare` и запуск упадёт с «Not logged in». Правильную переменную выдаёт только
интеграционный путь (`pkg/integrations/claude/integration_secrets.go`, коммит `a7130face`).

## Шаги узла

| # | тип | содержание |
|---|-----|-----------|
| 1 | bash | `PROD_SSH_KEY` → `~/.ssh/id_ed25519` (chmod 600), `ssh-keyscan` хоста в `known_hosts`, `git config user.name/user.email` |
| 2 | bash | клон `$REPO` по `GITHUB_TOKEN` в `repo/`, `git checkout main` |
| 3 | bash | реконсиляция: борды из очереди, уже лежащие в `sources/*.yml` на `origin/main`, переводятся в `onboarded` |
| 4 | bash | дамп рабочей очереди в `queue.tsv`; гарды (см. ниже) пишут маркер `SKIP` |
| 5 | bash | ветка `feat/onboard-contributed-boards-<YYYYMMDD>` |
| 6 | prompt | процедура онбординга бордов (инлайн) |
| 7 | bash | `go build ./...`, `git commit -s`, push, `gh pr create --label onboard-contributions` |
| 8 | prompt | (опц.) один новый адаптер для `review`-строки |
| 9 | bash | отдельная ветка + PR для адаптера |

### Шаг 3 — реконсиляция

Единственная запись в прод, и она производная от того, что уже в `main`:

```bash
ssh "$PROD_SSH_HOST" "sudo -u postgres psql -d $PROD_DB -tAF'|' -c \
  \"SELECT id, source, board FROM link_contributions WHERE status='pending';\"" > pending.tsv

while IFS='|' read -r id source board; do
  grep -qF "$board" "repo/sources/$source.yml" 2>/dev/null && echo "$id"
done < pending.tsv > merged_ids.txt
# → UPDATE link_contributions SET status='onboarded' WHERE id IN (...)
```

Борд считается онбордженным по факту наличия в `main`, а не по факту открытия PR. Незамерженный
PR прод не трогает — «автономно только до PR» соблюдено.

### Шаг 4 — рабочая очередь и гарды

Читаются три группы:

- `pending` — распознанные `(source, board)`, сгруппированные как в скилле
  (`min(id), source, board, min(url), count(*) … GROUP BY source, board ORDER BY min(created_at)`);
- `review` — нераспознанные ссылки (`source`/`board` NULL), лид-лист для новых адаптеров;
- `rejected` — перепроверяется **только когда `pending` и `review` пусты**, не больше 20 самых
  старых строк за прогон.

Перепроверка `rejected` каждый день по всей куче — это токены впустую: строки не меняются, а
дефект распознавателя, который их породил, чинится в коде. Ограничение сознательное; в отчёте
прогона пишется, сколько строк не смотрели.

Гарды пишут файл-маркер и **не** завершают шаг ненулевым кодом (это уронило бы узел):

- очередь пуста → `SKIP`;
- уже открыт PR с меткой `onboard-contributions` → `SKIP` (иначе за неделю накопится семь
  висящих веток, правящих один и тот же файл).

Prompt-шаг условно пропустить нельзя — он выполнится в любом случае. Поэтому первая строка
каждого промпта: «если существует файл `SKIP`, ничего не делай и ответь `NOTHING-TO-DO`».

### Шаг 6 — промпт онбординга

Процедура вшивается в промпт целиком: скилл `onboard-contributions` лежит в
`.claude/skills/`, а этот путь в hire заигнорен (`.gitignore:79`) — раннер клонирует
репозиторий и процедуры там не найдёт.

Промпт переносит из скилла: копировать `board` **побайтно** (иначе дедуп
`external_id LIKE '<board>:%'` не сматчится и борд задвоится), живая валидация каждого борда,
рецепты по провайдерам, порядок строк внутри файла (`zohorecruit.yml` отсортирован по `board`,
большинство остальных — append-only), поиск отображаемого имени компании
(`og:site_name` → `<title>` → JSON адаптера → гуманизированный слаг).

Валидация — только `curl`: `run.js:70` запускает CLI с `--allowedTools Bash,Read,Edit,Write`,
без `WebFetch` и `WebSearch`.

### Шаги 8–9 — новый адаптер

Борды и адаптеры разъезжаются по разным PR: список из двух строк YAML ревьюится за минуту,
новый Go-адаптер — это код с тестами, и общий diff блокирует дешёвое дорогим. Не больше одного
адаптера за прогон.

## install_params

| имя | тип | назначение |
|---|---|---|
| `repository` | `integration-resource` (github/repository, `useNameAsValue`) | целевой репозиторий |
| `machineType` | `string` | fleet раннера, напр. `e1-large-amd64` |
| `prodSshHost` | `string` | `user@host` продовой БД |
| `prodDbName` | `string` | имя базы, по умолчанию `hire` |
| `sshSecret` | `secret_picker` (`secretKey: private_key`) | приватный ключ для ssh на прод |
| `telegramChatId` | `string` | чат для итога прогона (для групп и каналов — отрицательный) |

Уведомления — `telegram.sendMessage`: freehire уже доставляет туда дайджесты, отдельная
интеграция не нужна. Бот должен состоять в чате, иначе узел упадёт на первом же прогоне.

## Требования к fleet раннера

`claude`, `git`, `gh`, `ssh`, `curl`, `go`. `psql` локально не нужен — запросы выполняются
на проде через ssh, как в скилле. Без Go шаг 7 не сможет собрать проект; для PR с бордами это
терпимо (CI соберёт), для адаптера — нет.

## Результат прогона

Результат — **отправленный PR**, а не отчёт о проделанной работе. Прогон считается успешным
ровно в двух случаях: открыт PR (ссылка уходит в Telegram) или очередь была пуста. Всё
остальное — падение узла, в том числе случай, когда агент отредактировал `sources/*.yml`, но
не смог запушить ветку или создать PR: шаг 7 проверяет, что `gh pr create` вернул URL, и
выходит с ненулевым кодом, если нет.

## Обработка ошибок

Ненулевой код любого шага уводит узел в канал **Failed** → уведомление с ссылкой на логи.
Пустая очередь — не ошибка: маркер `SKIP`, все шаги проходят вхолостую, узел завершается
**Passed**, уведомление говорит «очередь пуста».

Падение на конкретном борде не должно ронять прогон: промпт инструктирует пропустить борд
с пометкой в отчёте и продолжить со следующего.

## Приёмка

1. `superplane apps create --file canvas.yaml` (или установка фабрики) проходит валидацию.
2. Прогон при пустой очереди: узел **Passed**, PR не создан, прод не изменён.
3. Прогон с одной `pending`-строкой на живом борде: PR с одной строкой в `sources/<source>.yml`,
   `board` совпадает с таблицей побайтно, статус в проде не изменён до мержа.
4. Прогон с мёртвым бордом: PR не содержит этого борда, строка отмечена `rejected`.
5. Повторный прогон при открытом PR с меткой: `SKIP`, второй PR не создаётся.
6. Прогон после мержа PR из п.3: строка переходит в `onboarded`.

## Вне scope

- Начисление кредитов и промоушен `review`-строк (`credit_ledger`) — остаётся ручным.
- Замена скилла `onboard-contributions`: он остаётся для ручных прогонов.
- Настоящий ingest-воркер внутри hire — фабрика его не отменяет и не приближает.
