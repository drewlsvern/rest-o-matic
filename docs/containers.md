# Backing up container data

rest-o-matic has no container-specific source type yet. To back up the data
of a container, run rest-o-matic **on the container host** and list the
host side of its bind mounts under `paths:`, with absolute paths. For
example, a container started with
`-v /home/me/containers/gitea/data:/data` is backed up with
`paths: [/home/me/containers/gitea]`. Stop and start containers with hooks
(see [Hooks](configuration.md#hooks)) if their data must not change during the backup.
Named volumes aren't supported yet.

What decides whether this works is **who owns the files** the containers
write:

| Runtime | Run rest-o-matic as | `read_as` |
|---|---|---|
| Rootless Podman | the user that runs the containers | `podman-unshare` (see below) |
| Rootful Docker or rootful Podman | root | leave unset |
| Rootless Docker | — | not supported yet |

## Rootless Podman: `read_as: podman-unshare`

Rootless Podman maps users inside a container to your subordinate uids
(`/etc/subuid`), so a container's `postgres` user (uid 999) writes files
owned by something like uid 100998 on the host. When those files are
private (`0600`, or a `0700` directory, as Postgres uses), you can't read
them as yourself, and the backup fails with restic's exit code 3 and
`permission denied`. rest-o-matic adds a hint pointing here when that
happens.

`--userns=keep-id` maps the container's user to *you*, so a container
running as one user under keep-id writes files you can read. But a
container whose entrypoint runs as root before switching users, an image
running as a different uid, or a pod whose containers run as different
users still produces files you can't read. Rather than working out which
case applies, set:

```yaml
backups:
  gitea:
    source:
      paths: [/home/me/containers/gitea]
    read_as: podman-unshare
    policy: daily
    repositories: [nas]
```

restic then runs as `podman unshare restic backup …`, inside your rootless
user namespace, where every file your containers wrote is readable. This
needs no extra privilege, and works the same with or without keep-id. Only
the backup runs this way; hooks and retention don't. Snapshots taken this
way are tagged `restomatic-read=podman-unshare`. `read_as` is Linux-only:
on macOS and Windows, Podman runs containers in a VM and bind-mounted files
are already readable.

**Restore through the job**, so files come back with the owners they had.
Restore into an empty scratch directory first, then move what you need into
place:

```sh
# 1. restore into a scratch directory
rest-o-matic exec nas --job gitea -- restore <snapshot-id> \
  --target /home/me/restore --include /home/me/containers/gitea/data/app.ini

# 2. move it into place (inside the namespace, where the files are yours)
podman unshare mv /home/me/restore/home/me/containers/gitea/data/app.ini \
                  /home/me/containers/gitea/data/app.ini
podman unshare rm -rf /home/me/restore
```

Don't restore straight to `--target /`. restic also restores the owner and
permissions of every parent directory on the path (`/home`, `/home/me`,
…), which under `podman unshare` can't be set for `/` and `/home` (so the
restore reports errors), and goes wrong for your own directories if the
snapshot was read differently.

`--job` runs restic the way that job reads its files. A snapshot records
ownership as it was seen at backup time, so it must be restored the same
way. For a `podman-unshare` job, `exec --job` refuses (exit code 23) to
restore a snapshot that lacks the `restomatic-read=podman-unshare` tag,
and for `latest` it requires that tag in every `--tag` filter
(`--tag gitea,restomatic-read=podman-unshare`). Restore snapshots taken
before the job switched to `podman-unshare` with plain `exec`, without
`--job`. `restic mount` may not work under `--job`; use `restore`, `dump`
or `ls` instead.

## Rootful Docker or Podman: run as root

The containers already run as root, so run rest-o-matic as root too (from
root's crontab or a system timer). Root can read every file, and restores
bring back the original owners. Keep one user per state directory (see
[One user per state directory](concurrency.md#one-user-per-state-directory)).

If you'd rather not run the whole thing as root, you can let a normal user's
restic read every file instead. This is an advanced option:

```sh
sudo groupadd restic && sudo usermod -aG restic me
sudo chown root:restic /usr/local/bin/restic && sudo chmod 750 /usr/local/bin/restic
sudo setcap cap_dac_read_search+ep /usr/local/bin/restic
```

This lets members of the `restic` group read any file on the host through
restic, and nothing more. The capability is lost whenever the restic binary
is replaced, so reapply it after upgrades. Restoring files with their
original owners still needs root (`sudo rest-o-matic exec …`).
