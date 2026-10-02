# Installation

**Recommended for actually running backups** (a server, NAS, anything that
isn't just your own dev machine): download the pre-built binary from the
[Releases page](https://github.com/drewlsvern/rest-o-matic/releases). No Go
toolchain needed on that machine, it's the exact artifact that was built and
tested, checksums are provided, and `--version` on it reports full build
provenance (real commit hash and build time — see [Development](development.md#version)).

On Linux or macOS, [`install.sh`](../install.sh) does this for you. It
downloads the newest release for your OS and CPU, verifies its checksum,
and installs it into `~/.local/bin`, or into `/usr/local/bin` when run as
root:

```sh
curl -fsSL https://raw.githubusercontent.com/drewlsvern/rest-o-matic/main/install.sh | sh        # just you
curl -fsSL https://raw.githubusercontent.com/drewlsvern/rest-o-matic/main/install.sh | sudo sh   # system-wide
# a specific release, or a different directory:
curl -fsSL https://raw.githubusercontent.com/drewlsvern/rest-o-matic/main/install.sh | sh -s -- --version v0.0.1-rc.3 --dir ~/bin
```

`--list` shows the available releases. It's plain POSIX `sh` and needs only
`curl` or `wget`, `tar`, and `sha256sum` or `shasum`. Or do it by hand:

```sh
curl -LO https://github.com/drewlsvern/rest-o-matic/releases/download/<version>/rest-o-matic_<version>_<os>_<arch>.tar.gz
curl -LO https://github.com/drewlsvern/rest-o-matic/releases/download/<version>/checksums.txt
sha256sum --ignore-missing -c checksums.txt
tar -xzf rest-o-matic_<version>_<os>_<arch>.tar.gz
```

(`<os>_<arch>` is one of `linux_amd64`, `linux_arm64`, `darwin_amd64`,
`darwin_arm64`, or `windows_amd64` — see the Releases page for the current
`<version>` and exact filenames; Windows archives are `.zip` instead.)

On Windows, [`install.ps1`](../install.ps1) does the same from PowerShell
(Windows PowerShell 5.1 or PowerShell 7):

```powershell
irm https://raw.githubusercontent.com/drewlsvern/rest-o-matic/main/install.ps1 | iex
# a specific release, or a different folder:
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/drewlsvern/rest-o-matic/main/install.ps1))) -Version v0.0.1-rc.3 -InstallDir C:\Tools\rest-o-matic
```

It installs just for you into `%LOCALAPPDATA%\Programs\rest-o-matic`, or
for everyone into `Program Files\rest-o-matic` when run from an elevated
(administrator) PowerShell, and adds that folder to your user or the machine
`PATH` (`-NoPath` skips this). `-List` shows the available releases.

**For quickly trying a version on a machine that already has Go installed**:

```sh
go install github.com/drewlsvern/rest-o-matic/cmd/rest-o-matic@latest
```

This compiles from source fetched through Go's module system rather than
using the pre-built binary, so the only trade-off is that `--version`'s
commit/build-time fields show as "unknown" (see [Development](development.md#version)) — the
version number itself is still always correct.

Or build from source yourself:

```sh
go build -o rest-o-matic ./cmd/rest-o-matic
```

You'll also need the [`restic`](https://restic.net/) binary on your `PATH` —
rest-o-matic shells out to it rather than reimplementing any backend logic.
