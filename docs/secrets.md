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
