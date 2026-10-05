# Connecting to the central app

The central app shows every host's backups in one place. Each host reports
to it; the central app never connects to a host. A host that isn't
enrolled makes no request at all.

## Enrolling a host

In the central app, create the host. It shows a one-time command:

```sh
 rest-o-matic enrol https://backups.example.com --token 7GxK2mPq9sVw4tYb
```

Run it on the host as the user that runs your backups, in the directory you
run `tick` from, with the same `--config` and `--state-dir` your scheduler
uses. It:

- creates this host's key if there isn't one (see [Secrets](secrets.md));
- registers the host, sending its public key, hostname, operating system
  and the versions of rest-o-matic and restic;
- keeps the address, the host's ID and the credential it uses from now on
  in `enrolment.json` beside the host key, readable only by you.

The token works once and expires. Leave out `--token` to type it at a
prompt instead.

**Keeping the token out of your shell history.** The command starts with a
space, which bash leaves out of its history when `HISTCONTROL` is
`ignorespace` or `ignoreboth`. Debian and Ubuntu set `ignoreboth`; Fedora
sets only `ignoredups`, so there the token stays in your history unless you
add `HISTCONTROL=ignoreboth` to `~/.bashrc`. Leaving out `--token` avoids
the question.

**Only one config file reports.** The enrolment is for the config file
given when enrolling. Running `tick` with a different `--config`, a test
config say, sends nothing.

### HTTPS

The address must be `https://`, and the certificate must be valid. If it is
issued by a private authority, pass that authority's certificate with
`--ca-file ca.pem`; it is remembered for every later check-in. There is no
way to skip certificate checks. `--allow-http` permits a plain `http://`
address, for testing only: the credential is then sent unencrypted.

### Recovery keys

The central app may offer recovery public keys (see
[Secrets](secrets.md#re-locking)). Each one this host doesn't already have
is shown, and added to `recovery-recipients` only if you confirm it, or if
you named it with `--accept-recovery-key age1...`. Without a terminal, an
unnamed key is not added.

Values locked before a recovery key was added can't be opened with it.
When enrolment adds one and the config holds locked values, it offers to
re-lock them (`secret relock`), or prints the command to do so later.

### Enrolling again

`enrol` refuses to replace an existing enrolment. To move a host to a
different central app, or after it was deleted there, get a new token and
run `enrol` with `--force`. `rest-o-matic unenrol` removes the enrolment and
leaves the host key alone.

## What is sent, and when

Every `tick` begins with one **check-in**, before any job starts. It always
carries a fingerprint of three parts, and a part's content only when it has
changed since the central app last received it:

| Part | What it is | Sent when |
|---|---|---|
| status | Every job's state and recent runs, as `status <job> --json` shows them | A job ran, started, or became due |
| snapshots | Every job's recorded snapshot lists | A job's list changed |
| config | The config file, exactly as written | The file changed |

So a minute in which nothing happened costs one small request. A job that
finishes is reported by the next tick.

Reports include file paths, hostnames, hook commands and restic's error
messages, in plain text. Secrets are never sent readable: see below.

`rest-o-matic checkin` sends a check-in now and says which parts it
included. `rest-o-matic checkin --print` prints the full check-in, with
every part, without sending anything, and works without enrolling: it shows
exactly what the central app would receive.

## When the config is not sent

The config is backed up to the central app only when it holds no secret in
plain text. While any repository `password` or `env` value is written in
plain text, the config is withheld, and `status` and `checkin` say which
fields are the reason. Lock them with `rest-o-matic secret lock`.

Some `env` values are settings, not secrets, and may stay in plain text:

| Name | |
|---|---|
| `AWS_DEFAULT_REGION`, `AWS_REGION` | Where the bucket is |
| `RESTIC_COMPRESSION`, `RESTIC_PACK_SIZE`, `RESTIC_READ_CONCURRENCY` | restic's tuning |
| `RESTIC_CACHE_DIR`, `TMPDIR`, `GOMAXPROCS` | Paths and process settings |

For any other value that isn't secret, mark it `!plain`:

```yaml
    env:
      MY_BUCKET_PREFIX: !plain "host-a/"
```

Every other name counts as a secret, including account identifiers such as
`AWS_ACCESS_KEY_ID` or `B2_ACCOUNT_ID`. `password_file` and
`password_command` don't hold the secret itself, and don't count.

The central app never sees a locked value's plain text. It can replace a
locked value with a new one, locked in your browser for this host's key and
the recovery keys, but it can't read one.

## When the central app can't be reached

A check-in never fails a tick or stops its jobs. It gives up after 10
seconds, or 60 when it carries a part, and the tick carries on. Parts that
weren't received are sent again by the next tick.

One warning is printed when check-ins start failing, and one line when they
work again; nothing in between, so an outage costs two lines of output.
`status` shows the detail:

```
last tick: just now
central app: https://backups.example.com, last check-in 3 hours ago
  failing: dial tcp 100.64.0.5:443: connect: connection refused (last attempt just now)
```

If the central app no longer knows the host (it was deleted there), the
warning and `status` say that the host must be enrolled again.

## The format

The messages are defined in
[`contract/checkin/v1`](../contract/checkin/v1/README.md), with a JSON Schema
for each and examples.
