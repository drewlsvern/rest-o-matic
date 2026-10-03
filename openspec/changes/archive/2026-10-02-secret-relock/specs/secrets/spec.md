## ADDED Requirements

### Requirement: Re-locking Values
`rest-o-matic secret relock` SHALL open every locked value in the config and lock it again so that it can be opened by this host's key and by every key in the recovery recipients file, then write the config back. It SHALL open the values with the host key, or with the key file given by `--with-key`. It SHALL fail, writing nothing, when there is no host key.

The rewritten config SHALL differ from the original only in the locked values themselves: comments, YAML anchors and aliases, quoting, ordering and every other byte SHALL be unchanged. The file SHALL be replaced atomically and keep its permissions. The plain text of a value SHALL NOT be printed or written anywhere.

If any locked value can't be opened, nothing SHALL be written, and the command SHALL list each such value by repository and field and exit non-zero. `--dry-run` SHALL report what would be re-locked, and any value that can't be opened, without writing.

#### Scenario: Adding a recovery key after locking
- **WHEN** three values were locked with only the host key, a recovery key is then added to the recovery recipients file, and `secret relock` is run
- **THEN** all three SHALL afterwards open with the host key and, separately, with the recovery key

#### Scenario: Replacing a lost host
- **WHEN** a new host has its own key and a config whose values were locked for the old host and a recovery key, and `secret relock --with-key recovery.key` is run
- **THEN** every value SHALL afterwards open with the new host's key and with the recovery key

#### Scenario: Everything else is untouched
- **WHEN** the config has comments, a locked value shared through a YAML anchor, and inline maps, and `secret relock` is run
- **THEN** every line of the file that holds no locked value SHALL be unchanged, and the anchor and its aliases SHALL still be in place

#### Scenario: A value that can't be opened
- **WHEN** one of four locked values was locked for a key this host doesn't have
- **THEN** `secret relock` SHALL write nothing, name that value's repository and field, and exit non-zero

#### Scenario: Dry run
- **WHEN** `secret relock --dry-run` is run
- **THEN** it SHALL report how many values would be re-locked and write nothing

#### Scenario: No host key
- **WHEN** `secret relock --with-key recovery.key` is run on a host with no key of its own
- **THEN** it SHALL fail without writing, and say to create one with `secret keygen`
