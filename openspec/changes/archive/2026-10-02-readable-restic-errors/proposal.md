## Why

When restic fails, rest-o-matic reports what restic printed. Under `--json`, which the backup uses, restic prints its errors as JSON objects, so the reported error is a blob:

```
restic backup failed: exit status 10: {"message_type":"exit_error","code":10,"message":"Fatal: repository does not exist: unable to open config file: stat /mnt/nas/restic-repo/config: no such file or directory\nIs there a repository at the following location?\n/mnt/nas/restic-repo"}
```

That text is what `run` and `tick` print, what `status` shows and stores, and what hooks and notifications receive as `RESTOMATIC_ERROR`. It is hard to read in a terminal and worse in a phone notification.

## What Changes

- The error reported for a failed restic command carries restic's messages as plain text. JSON error objects are replaced by the message inside them; lines that are already plain text are kept.
- Only the first line of a multi-line message is kept, and a leading `Fatal: ` is dropped, since the error already says the command failed.
- When restic reports many errors (one per unreadable file, say), the first few are shown and the rest are counted.
- restic's exit status stays in the error, and the hint about unreadable source files is unchanged.
- This applies to the errors from backup, retention enforcement, and the snapshot lookup behind `exec --job`. Output from `exec` itself is still restic's own, untouched.

The same failure then reads:

```
restic backup failed: exit status 10: repository does not exist: unable to open config file: stat /mnt/nas/restic-repo/config: no such file or directory
```

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `backup-execution`: adds how a restic failure's message is reported.

## Impact

- `internal/execution`: one function that turns restic's standard error into messages, used where backup, forget and the snapshot lookup build their errors.
- The wording of error text changes everywhere it surfaces: command output, the state file, `status`, `RESTOMATIC_ERROR`. Anything matching on the old JSON text would need updating; matching on `exit status N` still works.
- No config or state format change.
