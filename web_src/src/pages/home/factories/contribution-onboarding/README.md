# Contribution Onboarding

A daily canvas that drains a crowdsourced board-contribution queue into a pull request.

Once a day the run reads the `link_contributions` table on production, validates every
contributed board live, adds the ones that answer to `sources/<source>.yml`, and opens a pull
request. A human merges it. At most one new ATS adapter is written per run, in a pull request
of its own.

The design and the reasoning behind each guard live in
[docs/design/contribution-onboarding-factory.md](../../../../../../docs/design/contribution-onboarding-factory.md).

## Installing it

The factory is registered in `../index.ts`, so it materializes through
`useInstallFactory({ factoryId: "contribution-onboarding" })`. The home page installs
`getFactoryDefinition()` with no argument, which is always the default factory — nothing in the
UI lists this one yet, so today it is installed programmatically or applied directly:

```bash
# canvas.yaml still carries {{ install_params.* }} placeholders; the CLI does not
# substitute them, so fill them in first.
superplane apps create --name "Contribution Onboarding" \
  --file canvas.yaml --file console.yaml
```

## What it needs

| Requirement | Why |
|---|---|
| Claude integration | The credential must arrive through the integration, not a bare secret: a secret-sourced credential is handed to the runner as `ANTHROPIC_API_KEY`, and a Claude Code subscription token in that variable is rejected by the CLI. |
| GitHub integration | `GITHUB_TOKEN` for the clone, the push and `gh pr create`. |
| Telegram integration | Where the run reports. The bot has to already be a member of the chat. |
| A secret holding an SSH private key under `private_key` | The contribution table lives only on production; every query goes over SSH. |
| A runner fleet with `claude`, `node`, `git`, `gh`, `ssh`, `curl` and the Go toolchain | `psql` is *not* needed — queries run on the production host. Without Go the run cannot build the repository before opening a PR. |

## Two things to know before editing the steps

**Steps are sourced into one shell.** `pkg/components/runner/claude/spec.go` runs each bash step
as `source "$SUPERPLANE_TASK_DIR/steps/N.sh"`. So `exit` tears down the whole run — use
`return` — and a `cd` leaks into every later step, which is why each step starts with
`cd "$RUN_ROOT"`. A test in `../materializeFactoryTemplate.spec.ts` guards the `exit` half.

**The last step must stay a prompt.** The finished event carries a single result — whatever the
last step wrote to `$SUPERPLANE_RESULT_FILE` — and the Telegram nodes read
`data[0].result.result`. The closing "Report the run" prompt is what puts a human-readable
summary there.
