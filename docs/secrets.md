# Secrets

A repository's `password`, and any value under its `env`, can be written in
a **locked** form instead of plain text:

```yaml
repositories:
  offsite:
    backend: s3
    url: "s3:https://s3.example.com/my-bucket/restic-repo"
    password: !locked "YWdlLWVuY3J5cHRpb24ub3JnL3YxCi0+IFgyNTUxOSA..."
    env:
      AWS_ACCESS_KEY_ID: AKIA...                    # plain, your choice per value
      AWS_SECRET_ACCESS_KEY: !locked "YWdlLWVuY3J5cHRpb24ub3JnL3YxCi0+..."
```

A locked value is encrypted for this host's key. rest-o-matic unlocks it in
memory when it starts restic and never writes the plain text to disk, so the
config file can be copied, kept in git or stored elsewhere without exposing
the credentials in it. Plain-text values keep working, and the two can be
mixed. `password_file` and `password_command` are unchanged.

**1. Make a recovery key first.** A value locked only for a host is lost if
that host's key is. Create a second key with [age](https://age-encryption.org),
keep its private half somewhere safe that isn't this host (a password
manager), and list its public half beside the host key:

```sh
age-keygen                       # prints a private key and its "public key: age1..."
mkdir -p ~/.config/rest-o-matic
echo "age1...the public key..." > ~/.config/rest-o-matic/recovery-recipients
```

Every value locked on this host from then on can also be opened with the
recovery key. The file takes one public key per line.

**2. Create the host's key.**

```sh
rest-o-matic secret keygen
```

It is written to `host.key` in your user config directory
(`~/.config/rest-o-matic/` on Linux), readable only by you, and is never
overwritten. `--key-file` uses a different path, with `recovery-recipients`
looked for in the same directory. The key is per user, so run this as the
user that runs your backups. A key file that other users can read is
refused.

**3. Lock a value and paste it into the config.**

```sh
rest-o-matic secret lock         # prompts for the value without showing it
pass show restic/offsite | rest-o-matic secret lock
```

It prints `!locked "..."`, which goes after `password:` or an `env` name.
To lock a value for a different host, add `--recipient <that host's public
key>` (from `rest-o-matic secret public-key` on that host).

Other commands:

- `rest-o-matic secret check` tries to unlock every locked value in the
  config and lists any this host can't open. Run it after copying a config
  to a host. (`validate` only checks that locked values are well-formed, and
  needs no key, so it gives the same result on any machine.)
- `rest-o-matic secret reveal <repository>` prints a locked password, and
  `secret reveal <repository> <ENV_NAME>` an env value, for running restic
  by hand.

## Re-locking

A locked value can only be opened by the keys it was locked for. When that
set of keys changes, `secret relock` opens every locked value in the config
and locks it again for this host's key and every key in
`recovery-recipients`, then writes the config back. Only the locked values
change: comments, anchors and the rest of the file stay exactly as they
were. If any value can't be opened, nothing is written and each one is
listed. `--dry-run` reports what would happen without writing.

It's needed in three situations:

- **A recovery key was added after values were locked.** Those values can't
  be opened with it yet. Add the key to `recovery-recipients`, then run
  `rest-o-matic secret relock`.
- **A lost host is replaced.** On the new host, set up `recovery-recipients`
  and run `secret keygen`, copy the config over, then open the values with
  the recovery key:

  ```sh
  rest-o-matic secret relock --with-key /path/to/recovery.key
  ```

  The key file must be readable only by you. Delete it from the host
  afterwards; nothing needs it again.
- **This host's key is replaced.** Move the old key aside, create a new one
  with `secret keygen`, and run `secret relock --with-key <the old key>`.

To restore a lost host's backups without setting up a new host first, point
`--key-file` at the recovery key; every command then uses it as the host
key:

```sh
rest-o-matic --key-file /path/to/recovery.key exec offsite -- restore latest --target /restore
```

## Values that aren't secret

An `env` value can be marked as deliberately not secret:

```yaml
    env:
      MY_BUCKET_PREFIX: !plain "host-a/"
```

It is used exactly as if it had no marker; the marker only records the
decision, for tools that treat unmarked plain-text values as secrets that
haven't been locked yet. It is allowed only on `env` values.

## When a value can't be unlocked

A repository whose locked value can't be unlocked fails without restic being
started, and says whether the host has no key or the value was locked for a
different one.

Locked values are ordinary age files, base64-encoded, so they can be opened
without rest-o-matic:

```sh
echo '<the text between the quotes>' | base64 -d | age --decrypt -i <key file>
```

What locking does and doesn't protect: it protects the config once it leaves
the host. It does not protect against someone who can already read files as
the backup user there, since they can read the host key too. Only `password`
and `env` values can be locked; if a repository `url` contains credentials,
move them into `env`.
