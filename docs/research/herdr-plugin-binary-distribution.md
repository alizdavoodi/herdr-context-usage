# How Herdr plugins distribute compiled binaries

Checked 2026-10-16 against Herdr v0.9.5 and each plugin's default branch on that
date. Commit-pinned links are in [Sources](#sources).

## TL;DR

**Recommendation: keep `go build` as the only `[[build]]` path while the repo is
private. When it goes public and the first tag is cut, wrap the build in a
small script that builds from source when Go is on `PATH` and otherwise
downloads the release asset for the manifest's `version`.** The download is
checked against `SHA256SUMS`, and against a GitHub attestation when `gh` is
present. Enforce tag = manifest `version` in CI, and never fall back to
`latest`.

Why:

- **No one can use a fallback yet.** Anonymous `curl` cannot download release
  assets from a private repo. Artifact attestations need a public repo on
  Free/Pro/Team plans. The README already sends users to `plugin link`, which
  runs no build at all.
- **Source-first keeps the trust model intact for everyone who has Go.** Herdr's
  install preview shows the `[[build]]` argv, not what a script downloads. A
  downloaded binary is not the source the user previewed, so it should be the
  exception (no Go), not the common path.
- **Most of the ecosystem already works this way.** About 2 in 5 compiled
  Herdr plugins download release assets from `[[build]]`. Most of those read
  the version from `herdr-plugin.toml` and check a SHA-256. Very few verify
  provenance (8 of 230).
- **Herdr will not do this for you.** A proposal for a native `[[dist]]`
  manifest key (herdrdev/herdr#2693) was closed `NOT_PLANNED` and moved to
  Discussions (#2696), where it has had no replies. Herdr resolves only the OS
  (`linux`/`macos`/`windows`), never the CPU architecture.

## Method and caveats

- **Population.** GitHub topic `herdr-plugin` held 1,775 repositories. Of
  these, 608 have Go, Rust, Swift, Zig, C or C++ as their primary language:
  Go 190, Rust 403, Swift 13, others 3. 591 of those 608 have at least one
  `herdr-plugin.toml` on the default branch, and 562 declare at least one
  `[[build]]`.
- **Classification.** Each manifest was parsed and every script its `[[build]]`
  argv names was fetched, following script references one level deeper. The
  scripts were then grepped for release URLs, checksum tools and verifiers.
  These are heuristic grep counts. Treat them as approximate, and as lower
  bounds for scripts nested two or more levels deep.
- **Repo language** is GitHub's primary-language guess. A TypeScript repo that
  ships a compiled binary is not counted.
- **No foreign code was executed.** Manifests and scripts were read as text
  only.

## 1. Compiled Herdr plugins and how `[[build]]` gets the binary

Of the 562 compiled plugins that declare a `[[build]]`:

| Strategy | Count | Notes |
|---|---|---|
| Build from source only (`cargo build`, `go build`, or a script that only compiles) | ~332 | includes the official cookbook's `rust-release-check` |
| Downloads a GitHub release asset (with or without a source fallback) | ~230 | Go 89 of 178, Rust 141 of 374 |
| …of which also contain a source-build path | ~170 | order varies, see below |
| …of which check a SHA-256 | ~199 | almost always against `SHA256SUMS` from the same release |
| …of which verify provenance or a signature | 8 | `gh attestation verify`, `cosign verify-blob`, or `ssh-keygen -Y verify` |
| …of which pin hashes in the source tree | 6 | |
| …of which read the version from `herdr-plugin.toml` | ~157 | rather than `latest` (~31) |
| No `[[build]]` (expects a binary on `PATH`) | 29 | e.g. `pinkpixel-dev/quota`, `uwuclxdy/clauth` |

Go plugins download release assets more often than Rust plugins (50% vs 38%).
CGO-free cross-compilation is cheap, and a Go user is less likely than a Rust
user to have the toolchain installed.

### The patterns, with their exact `[[build]]` entries

**A. Compile in place.** The default in Herdr's own docs and example cookbook.
This is the most common pattern.

```toml
# ogulcancelik/herdr-plugin-examples rust-release-check, kryptamine/herdr-auto-title (Go)
command = ["cargo", "build", "--release"]
command = ["go", "build", "-o", "herdr-auto-title", "./cmd/herdr-auto-title"]
```

**B. Source first, download fallback.** This is what `docs/design.md` describes.

- `senna-lang/herdr-agent-usage` (Go): `command = ["bash", "bin/ensure-binary.sh", "--in-tree"]`.
  - The script runs `go build`. On failure it fetches `usagebar-<goos>-<goarch>`
    for `v<manifest version>`, then **falls back to `latest`**.
  - It tries `gh release download` first, because "gh works for private and
    public repos", then `curl`.
  - It runs the downloaded binary (`"$tmp" version`) as a sanity check.
  - It does **not** check the `SHA256SUMS` its own release publishes.
  - The manifest comment explains why this exists at all: reinstall replaces the
    checkout, "which never carries the gitignored bin/usagebar" (their
    issue #48).
- `cloudmanic/herdr-plus` (Go): `command = ["sh", "scripts/build.sh"]`. It runs
  `go build` if `go` exists, otherwise `install.sh`, which downloads the
  **latest** GoReleaser archive with no version pin.
- `cyperx84/herdr-loop` (Go): source first. The fallback is
  `VERSION="v0.1.0"` plus a per-target SHA-256 table committed in
  `scripts/build.sh`, because "a checksum fetched from the same release the
  binary came from proves nothing about tampering in transit or a compromised
  release".

**C. Download first, source fallback.**

- `smarzban/herdr-file-viewer` (Rust):
  `command = ["/bin/sh", "scripts/fetch-or-build.sh"]`.
  - It maps `uname -s`/`uname -m` to a target triple and reads the version from
    `Cargo.toml`.
  - It downloads `v$version/<asset>` plus `SHA256SUMS` and verifies them.
  - On any miss it runs `cargo build --release`.
  - It matches by version, not commit, on purpose: a checkout ahead of the tag
    still gets the released binary.
  - About 47 plugins carry a file named `scripts/fetch-or-build.sh`, many of
    them close copies of this one.
- `nicosuave/memex`, `ChmaraX/herdr-nvim` and `eliasstravik/herdr-projects`
  work the same way. `herdr-projects` falls back from `curl`/`wget` to
  `gh release download` ("A private repo's release is not handed out without a
  login"), and verifies a `.sha256` sidecar.

**D. Download only, no source fallback.**

- `permgps/herdr-telegram-agents` (Go, GoReleaser):
  `command = ["sh", "scripts/install.sh"]`.
  - It reads `version` from the manifest and forces `curl --proto =https`.
  - It checks `checksums.txt`. It then verifies an SSH signature (`ssh-keygen -Y
    verify`) over a signed `release.txt` against an `allowed_signers` file
    **committed in the repo**.
  - It refuses to install unless the signature verifies or the user supplied an
    approved checksum.
- `plannotator/herdr-annotate`, `persiyanov/herdr-reviewr` and
  `aorumbayev/herdr-workflows` download and check the release `SHA256SUMS` or
  a sidecar, then fail on unsupported platforms. `herdr-reviewr` publishes
  build-provenance attestations but its install script does not verify them.

**E. Get the binary from somewhere else.**

- `openclaw/crabbox` requires `crabbox` on `PATH`.
- `furkankly/zoetrope` installs via `brew` or `cargo install zoetrope`.
- `eugenioenko/ttt` pipes `curl …/main/install.sh | sh`.
- `quaywin/agys` does the same inline in the manifest:
  `"sh","-c","… curl -fsSL https://raw.githubusercontent.com/quaywin/agys/main/install.sh | bash …"`.
  The script that runs comes from `main` at install time, not from the
  previewed commit.

**Anti-pattern.** `RanolP/herdr-handsfree` hard-codes one target, so it breaks
on every other OS and architecture:

```toml
command = ["sh", "-c", "mkdir -p target/release && curl -fsSL https://github.com/RanolP/herdr-handsfree/releases/download/v0.2.0/herdr-handsfree-v0.2.0-aarch64-apple-darwin.tar.gz | tar xz -C target/release"]
```

### How they pick OS and architecture

Every downloader does it in shell: `uname -s` gives `Darwin`/`Linux`, and
`uname -m` gives `x86_64`/`arm64`/`aarch64`. These are mapped to Go
`GOOS`/`GOARCH` names or Rust triples. Herdr cannot help here. Its build filter
compares an item's `platforms` with the OS Herdr was compiled for, and has no
architecture concept (`current_plugin_platform()`, `src/cli/plugin.rs`). Rust
plugins usually ship `*-linux-musl` so a single Linux build covers glibc and
musl. A CGO-free Go binary gets the same result by being static.

### The usage and context plugins `docs/design.md` alludes to

Only one is identifiable from repo history: `senna-lang/herdr-agent-usage`
(ADR-0003 names it). It is the "most mature" one. Its manifest describes
"context meters, provider limit windows, and rate-limit toasts", and it has an
update checker. It builds source first with a download fallback (pattern B).

**The other four could not be confirmed.** `docs/design.md` and the ADRs do not
name them, and no transcript of the 2026-08-16 design session survives. These
repos predate that date and report usage:

| Repo | Language | Build |
|---|---|---|
| `Davidcreador/herdr-token-dashboard` | Go | `./scripts/build.sh` → `go build` |
| `uwuclxdy/clauth` | Rust | manifest added 2026-08-13; no `[[build]]`, expects `clauth` on `PATH` |
| `quaywin/agys` | Go | `curl … main/install.sh \| bash` |
| `alejodelosrios/herdr-claude-usage` | Python | none needed |
| `iamhouser/herdr-claude-usage-multi` | Python | none needed |

`pinkpixel-dev/quota`'s Herdr manifest first appears on 2026-09-10, so it is
not one of the five.

## 2. Release tooling and version sync

**Tooling, among the ~230 downloaders.** Every count below is a lower bound.

| Tool | Count |
|---|---|
| A release workflow file | almost all |
| GoReleaser | ≥34 |
| `taiki-e/upload-rust-binary-action` | ≥16 |
| release-please | ≥9 |
| `actions/attest-build-provenance` | ≥26 |
| cargo-dist | 1 |

The rest use a hand-written matrix plus `gh release create`/`upload`.
`senna-lang` is a typical Go example: a four-target `CGO_ENABLED=0 GOOS=… GOARCH=… go build`
loop, `shasum -a 256 > SHA256SUMS`, then `gh release create`.

**Why version sync matters.** About 157 downloaders read `version` from
`herdr-plugin.toml` to choose the release tag. If the manifest and the tag
disagree, the download 404s or fetches the wrong binary. Four ways of keeping
them in step:

1. **Check before tagging (local script).** `senna-lang`'s `scripts/release.sh`
   refuses unless `herdr-plugin.toml` version == the tag, then tags and pushes.
2. **Fail the release job on mismatch (CI on `v*` tag push).**
   - `smarzban/herdr-file-viewer` checks tag = `Cargo.toml` = manifest.
   - `alexarthurs/herdr-sidebar` checks those plus `Cargo.lock`.
   - `eliasstravik/herdr-projects` and `permgps` (`scripts/check-version.sh`)
     do the same.

   This is the cheapest guard.
3. **CI writes the version, then tags.**
   - `cloudmanic/herdr-plus` seds both `version.go` and the manifest, commits,
     and tags.
   - `aorumbayev/herdr-workflows` uses go-semantic-release plus a
     `prepare-release` stamper.
   - `0xGosu/herdr-auto-pilot` bump-merges, then tags. It documents a ~15-minute
     window in which `main` names a version whose assets are not built yet, and
     covers it by falling back to an earlier release.
4. **release-please `extra-files`**, with
   `{"type":"toml","path":"herdr-plugin.toml","jsonpath":"$.version"}`
   (`ChmaraX/herdr-nvim`, `kryptamine/herdr-auto-title`).

**Why `latest` is worse than the manifest version.** `plugin install --ref`
can install old or new source. A `latest` download (about 31 plugins) then
pairs that source with a binary from a different version. `senna-lang` tries
the manifest version first and only then `latest`. `cloudmanic` uses only
`latest`.

## 3. Other ecosystems

- **zellij** loads plugins as `.wasm` from `file:`, `zellij:`, or
  `http(s)://…/plugin.wasm` URLs. Plugins request permissions at runtime.
  *Implication:* zellij avoids both problems this repo has. Wasm is one
  artifact for every CPU, and a sandbox replaces source review. Herdr plugins
  are native processes running as the user, so neither carries over.
- **blink.cmp** (Neovim, Rust core).
  - It downloads `releases/download/<tag>/<triple><ext>` and its `.sha256` at
    **runtime**, and only when the checkout sits **exactly on a git tag**
    (`git describe --tags --exact-match`).
  - On `main` it refuses unless the user sets `force_version`, and recommends
    `build = 'cargo build --release'` through the plugin manager.
  - If the download fails it falls back to a pure-Lua matcher, not to cargo.
  - The checksum comes from the same release.

  *Implication:* tie the binary to the exact version of the checked-out
  source, and let users who track `main` build from source.
- **telescope-fzf-native.nvim** says "we do not ship binaries". Users add a
  `build = 'make'` or cmake hook in their plugin manager. *Implication:*
  source-only is a respectable, maintained choice when the toolchain is
  ubiquitous.
- **tmux TPM** clones plugins and executes their `*.tmux` files. It has no build
  hook. Compiled plugins do it themselves. `tmux-thumbs`' installer asks
  "Compile" (cargo) or "Download" (latest release, no checksum). *Implication:*
  a prompt is not possible under Herdr, which runs builds with `stdin` set to
  null, so a script has to decide on its own.

## 4. What Herdr itself says and does (v0.9.5)

### Docs

- **Builds run once, at install, with the user's environment.** "Build commands
  run during GitHub `plugin install` after confirmation and before Herdr
  registers the plugin. If a build command fails, install aborts". They "do not
  receive runtime plugin context or Herdr socket env", and "Herdr reports build
  failures but does not install missing toolchains". `plugin link` runs none.
- **A build must not touch the manifest.** "changing `herdr-plugin.toml` after
  the install preview aborts install."
- **Trust rests on the user reading the source.** "skim the `herdr-plugin.toml`
  manifest and the scripts or binaries it runs. `herdr plugin install` shows a
  preview".
- **Install and update:**
  - `plugin install` "accepts GitHub shorthand only… clones with `git`".
  - `plugin update` "refetches the ref recorded during install, skips unchanged
    commits".
  - `plugin update` shipped in **0.9.4** (CHANGELOG, #4245).
- **The marketplace lists public repos only.** It indexes "public GitHub
  repositories" tagged `herdr-plugin`.

### Source (`src/cli/plugin.rs` @ v0.9.5)

- **The preview shows argv only.** It prints each build command's argv
  (`build: …`), not script contents or URLs (L1424–1434).
- **The build runs in the final installation directory.** The checkout is
  renamed into the installation directory, the build runs there, and the
  manifest is reloaded and compared with the previewed one (L374–393,
  L1480–1490).
- **The build is non-interactive and quiet on success.** `stdin` is null.
  stdout/stderr are tail-captured and shown **only on failure**, so a success
  notice like "installed prebuilt vX" is never seen (L1492–1559).
- **Herdr's own env vars are stripped.** `HERDR_*` socket, session and pane
  variables, and all `HERDR_PLUGIN_*`, are removed. Everything else, including
  `PATH` and `GH_TOKEN`, is inherited (L1695–1713).
- **The clone is shallow and anonymous-capable.** It is
  `git init` + `fetch --depth 1 origin <ref|HEAD>` + detached checkout from
  `https://github.com/<owner>/<repo>.git` (L953–955, L1078–1092). The checkout
  has no tags, so `git describe` cannot recover the version. The manifest
  `version` is the only version source. One plugin, `bon5co/bermuda`, works
  around this with a `git fetch --depth 1 origin refs/tags/*:refs/tags/*` build
  step.
- **Update skips an unchanged commit.** If the resolved commit equals the
  installed one, update prints "already up to date" and runs no build
  (L348–356). Otherwise it runs the full install path, preview and build
  included.

### Issues and Discussions

- **#2693, "Plugin manifests: optional prebuilt binaries for compiled plugins"**
  (2026-08-12), was closed `NOT_PLANNED` within two minutes by the triage bot
  under the bugs-only issue policy.
  - It proposed `[[dist]]` entries with `platforms`, `arch`, `url`, `sha256` and
    `path`. Herdr would download and verify, show the URL and hash in the
    preview, and fall back to `[[build]]`.
  - Its own motivation: "You can already do this with a build command that
    shells out to curl, but then every author writes their own
    download-and-verify script and the preview shows `sh scripts/install.sh`
    instead of a url and a hash."
  - It was re-posted as Discussion **#2696** (Ideas): 1 upvote, 0 comments, open.
  - No maintainer response has been found.
- **#2925.** The bot's answer to a `cargo`-missing install failure: "Herdr
  intentionally uses build tools already available on the invoking process's
  `PATH`; it does not install missing toolchains… The plugin should also
  document Cargo as a prerequisite."
- **Nothing found** for an arch selector, a release-asset fetch, or build
  sandboxing in issues, discussions, the CHANGELOG or the docs.

## 5. Recommendation for herdr-context-usage

**Now (private, no tags, no CI): change nothing.**

- A release-asset path cannot serve anyone but the owner.
- GitHub serves release assets only to accounts with read access. Anonymous
  `curl` 404s.
- Attestations "are only available for public repositories" on Free, Pro and
  Team plans.
- `plugin install` is documented against public repos. The README already
  routes users to `plugin link`, which never runs `[[build]]`.
- Whether `plugin install` of a private repo works through the user's git
  credential helper was not tested. The clone runs plain `git` with the
  inherited environment, so it plausibly does for the owner.

**At go-public (first tag `v0.1.0`):**

1. **Release workflow on `v*` tags.**
   - Fail unless `${GITHUB_REF_NAME#v}` equals the manifest `version` (one
     grep).
   - Cross-compile `darwin/{arm64,amd64}` and `linux/{amd64,arm64}` in a shell
     loop with the *same* flags as today's `[[build]]`
     (`CGO_ENABLED=0 -trimpath -ldflags=-s -w`).
   - Write `SHA256SUMS`, run `actions/attest-build-provenance`, then
     `gh release create`.
   - `modernc.org/sqlite` is pure Go, so one Ubuntu runner covers all four
     targets.
   - Skip GoReleaser. It earns its config when you want archives, changelogs or
     Homebrew. For one bare binary, the senna-lang-style loop is about 10
     lines with no extra pinned dependency.
2. **`[[build]] = ["sh", "scripts/build.sh"]`:**
   - If `go` is on `PATH`, run exactly today's command. This is the exact
     previewed source, and its modules are checked against the committed
     `go.sum`.
   - Otherwise map `uname` to the asset name and download
     `v<manifest version>/herdr-context-usage-<os>-<arch>` plus `SHA256SUMS`
     over https.
   - Verify the hash. If `gh` is present, also run
     `gh attestation verify --repo alizdavoodi/herdr-context-usage`.
   - Install to `./herdr-context-usage`, the path the six manifest commands and
     `manifest_test.go` already assume.
   - On any miss, fail with a message naming Go as the fix. Never fall back to
     `latest`.
   - Keep `platforms = ["linux", "macos"]`.

**Trade-offs this accepts:**

- **Trust.** The preview now shows `sh scripts/build.sh` instead of the
  `go build` argv. A careful user must read one more file. Users with Go still
  run exactly what they previewed.
  - For users without Go, `SHA256SUMS` from the same release proves integrity
    only. `jtnovellis/herdr-worktree-setup` and `cyperx84/herdr-loop` both
    say so explicitly.
  - The attestation binds the binary to this repo's CI, not to the previewed
    commit.
  - The only fully commit-bound option for downloads is the hash table pinned
    in source (`cyperx84`, `Qu4tro/herdr-whichkey`). It costs a follow-up
    commit per release, because the tagged commit cannot contain hashes of its
    own build. That is not worth it for a fallback.
  - Download-first (pattern C) would make the *common* path run unpreviewed
    bytes in exchange for install speed. That is the wrong trade for a
    single-binary Go plugin.
- **Go toolchain.** The `go 1.22` requirement remains for the default path.
  The fallback removes it as a hard gate, which is the problem
  `docs/design.md` names: "a failed build aborts the install".
- **Version and tag sync.** One CI check (pattern 2.2) is enough. Bump
  `version` only in the commit you tag, so the version on `main` never names a
  release with no assets. This avoids the window 0xGosu documents.
- **Architecture coverage.** The four targets match what `platforms` allows.
  Anything else (for example `linux/riscv64`) gets a clear "install Go"
  failure rather than a wrong binary.
- **`plugin update`.**
  - Every update is a fresh checkout without the gitignored binary, so
    `[[build]]` runs again. The script must stay idempotent and must not edit
    the manifest.
  - Update skips unchanged commits, so cutting a tag on an existing commit
    pushes nothing to users. They pick up the next commit.
  - Matching by version means a no-Go user installing `main` ahead of the tag
    gets the tagged binary, which is older than the source they previewed.
    This is acceptable if `version` is bumped only at release commits. The
    stricter alternative compares `git rev-parse HEAD` with a commit marker
    published with the release (as `smarzban` does informationally). It leaves
    no-Go users on untagged commits with no fallback at all.
  - Because Herdr discards build output on success, record which path ran
    (for example a one-line `build-receipt` file, as `permgps` writes
    `bin/install-receipt`) if you ever need to debug a user's install.

## Summary table: representative Herdr plugins

Stars and commits as of 2026-10-16. "Fallback" means a source build when the
download fails.

| Repo | Lang | ★ | Build strategy | Verification | Release workflow | Version sync |
|---|---|---|---|---|---|---|
| openclaw/crabbox | Go | 1472 | binary on `PATH` required (E) | n/a | GoReleaser (CLI) | n/a |
| furkankly/zoetrope | Rust | 1048 | `brew`/`cargo install` if missing (E) | n/a | release-plz | n/a |
| persiyanov/herdr-reviewr | Rust | 895 | download only (D), by manifest version | `.sha256` sidecar | taiki-e + attest (not verified at install) | build reads manifest |
| eliasstravik/herdr-projects | Rust | 779 | download, then cargo (C), `curl` then `gh` | `.sha256` sidecar | matrix + `gh release` | CI fails if tag ≠ Cargo/manifest |
| plannotator/herdr-annotate | Rust | 669 | download only (D) | `SHA256SUMS` | matrix + `gh release` | CI checks tag = Cargo = `.version` file |
| smarzban/herdr-file-viewer | Rust | 658 | download, then cargo (C), version-only match | `SHA256SUMS` | matrix + `gh release` | CI fails if tag ≠ Cargo/manifest |
| eugenioenko/ttt | Go | 600 | `curl main/install.sh \| sh`, latest (E) | none | softprops | n/a |
| alexarthurs/herdr-sidebar | Rust | 475 | download, then cargo (C) | `SHA256SUMS` | matrix + `gh release` | CI checks tag = Cargo = lock = manifest |
| cloudmanic/herdr-plus | Go | 353 | go build, then latest download (B) | none found | GoReleaser | CI seds manifest and `version.go`, then tags |
| ChmaraX/herdr-nvim | Rust | 282 | download, then cargo (C) | none found | softprops | release-please `extra-files` |
| nicosuave/memex | Rust | 266 | download, then cargo (C) | `.sha256` | matrix + `gh release` | build reads manifest |
| kryptamine/herdr-auto-title | Go | 256 | `go build` (A) | n/a | release-please | release-please `extra-files` |
| smarzban/tsk | Rust | 213 | `cargo build --release` (A) | n/a | yes | n/a |
| permgps/herdr-telegram-agents | Go | 80 | download only (D) | checksums + SSHSIG against committed signers | GoReleaser (pinned by SHA) | `check-version.sh` in CI |
| senna-lang/herdr-agent-usage | Go | 53 | go build, then manifest-version, then latest download (B) | none (publishes `SHA256SUMS`) | 4-target loop + `gh release` | `release.sh` checks before tagging |
| Davidcreador/herdr-token-dashboard | Go | 25 | `go build` (A) | n/a | softprops | n/a |
| aorumbayev/herdr-workflows | Go | 32 | download only (D) | `checksums.txt` | GoReleaser + semantic-release | CI stamps version |
| 0xGosu/herdr-auto-pilot | Go | 25 | download only, then earlier release (D) | `SHA256SUMS` | custom + bump bot | CI bump-merges, then tags |
| cyperx84/herdr-loop | Go | 5 | go build, then download (B) | **hashes pinned in source** | matrix + `gh release` | manual, same commit |
| Qu4tro/herdr-whichkey | Rust | 0 | download, then cargo (C) | **hashes pinned in source** | matrix + `gh release` | manual |
| m-mohamed/sheprd | Rust | 1 | download, then cargo (C) | sha256 + **`gh attestation verify` required** | taiki-e + attest | CI |
| ardasevinc/herdr-codex-bridge | Go | 3 | download only (D) | checksums + **cosign** (identity pinned to workflow@tag) when installed | GoReleaser | CI |
| joshuadavidthomas/hrd | Go | 3 | download only (D), latest unless pinned | sha256 + optional attestation (`HRD_REQUIRE_ATTESTATION`) | GoReleaser + attest | n/a |
| quaywin/agys | Go | 11 | `curl main/install.sh \| bash` inline (E) | none in manifest | GoReleaser | n/a |
| RanolP/herdr-handsfree | Rust | 2 | hard-coded aarch64-darwin URL | none | `gh release` | hard-coded |
| uwuclxdy/clauth, pinkpixel-dev/quota | Rust | 244, 29 | no `[[build]]`; binary on `PATH` | n/a | n/a | n/a |

## Sources

Herdr (tag `v0.9.5`):

- Plugins docs, build commands and trust: <https://github.com/herdrdev/herdr/blob/v0.9.5/docs/next/website/src/content/docs/plugins.mdx#L35-L47>, <https://github.com/herdrdev/herdr/blob/v0.9.5/docs/next/website/src/content/docs/plugins.mdx#L113-L121>, <https://github.com/herdrdev/herdr/blob/v0.9.5/docs/next/website/src/content/docs/plugins.mdx#L192-L245>
- Marketplace: <https://github.com/herdrdev/herdr/blob/v0.9.5/docs/next/website/src/content/docs/marketplace.mdx>
- Install/build source: <https://github.com/herdrdev/herdr/blob/v0.9.5/src/cli/plugin.rs#L348-L393>, <https://github.com/herdrdev/herdr/blob/v0.9.5/src/cli/plugin.rs#L953-L955>, <https://github.com/herdrdev/herdr/blob/v0.9.5/src/cli/plugin.rs#L1078-L1092>, <https://github.com/herdrdev/herdr/blob/v0.9.5/src/cli/plugin.rs#L1424-L1434>, <https://github.com/herdrdev/herdr/blob/v0.9.5/src/cli/plugin.rs#L1460-L1559>, <https://github.com/herdrdev/herdr/blob/v0.9.5/src/cli/plugin.rs#L1695-L1733>
- CHANGELOG (`plugin update` in 0.9.4): <https://github.com/herdrdev/herdr/blob/v0.9.5/CHANGELOG.md#L17-L20>
- Issues and Discussions: <https://github.com/herdrdev/herdr/issues/2693>, <https://github.com/herdrdev/herdr/discussions/2696>, <https://github.com/herdrdev/herdr/issues/2925>, <https://github.com/herdrdev/herdr/discussions/3219>
- Example cookbook: <https://github.com/ogulcancelik/herdr-plugin-examples/blob/18709cdc851dd63ed0543eb8388343a5446fd8d8/rust-release-check/herdr-plugin.toml>

Plugins (commit-pinned):

- senna-lang/herdr-agent-usage:
  - <https://github.com/senna-lang/herdr-agent-usage/blob/fc01eab38a12d6c435ac3cf31e4b449ac1234aa0/herdr-plugin.toml#L9-L18>
  - <https://github.com/senna-lang/herdr-agent-usage/blob/fc01eab38a12d6c435ac3cf31e4b449ac1234aa0/bin/ensure-binary.sh#L35-L130>
  - <https://github.com/senna-lang/herdr-agent-usage/blob/fc01eab38a12d6c435ac3cf31e4b449ac1234aa0/.github/workflows/release.yml#L60-L80>
  - <https://github.com/senna-lang/herdr-agent-usage/blob/fc01eab38a12d6c435ac3cf31e4b449ac1234aa0/scripts/release.sh#L20-L23>
  - <https://github.com/senna-lang/herdr-agent-usage/issues/48>
- cloudmanic/herdr-plus: <https://github.com/cloudmanic/herdr-plus/blob/f38df3570bea8f7ca71dc1ba11bce3b123d14402/scripts/build.sh>, <https://github.com/cloudmanic/herdr-plus/blob/f38df3570bea8f7ca71dc1ba11bce3b123d14402/install.sh#L106-L123>, <https://github.com/cloudmanic/herdr-plus/blob/f38df3570bea8f7ca71dc1ba11bce3b123d14402/.github/workflows/release.yml#L100-L136>
- cyperx84/herdr-loop: <https://github.com/cyperx84/herdr-loop/blob/af24ceef312f856ba1b099d4ae0ba42c04a88b9f/scripts/build.sh#L1-L90>
- Qu4tro/herdr-whichkey: <https://github.com/Qu4tro/herdr-whichkey/blob/7c40df73a2de271ee9e077c1518e531fe17cc566/scripts/build.sh#L9-L25>
- smarzban/herdr-file-viewer: <https://github.com/smarzban/herdr-file-viewer/blob/f2114a9357021cb7df6851e6d25a2576f29ab5ad/scripts/fetch-or-build.sh>, <https://github.com/smarzban/herdr-file-viewer/blob/f2114a9357021cb7df6851e6d25a2576f29ab5ad/.github/workflows/release.yml#L31-L46>
- alexarthurs/herdr-sidebar: <https://github.com/alexarthurs/herdr-sidebar/blob/081ed133f3faa377438b51ce435fe8a87c439593/.github/workflows/release.yml#L33-L42>
- eliasstravik/herdr-projects: <https://github.com/eliasstravik/herdr-projects/blob/963d81b4523f725ee028b893521811afd72d1369/bin/run.sh#L48-L102>, <https://github.com/eliasstravik/herdr-projects/blob/963d81b4523f725ee028b893521811afd72d1369/.github/workflows/release.yml#L46-L50>
- persiyanov/herdr-reviewr: <https://github.com/persiyanov/herdr-reviewr/blob/859dd5b11a5280fe282de56b52378b20a54fa10a/herdr/install.sh#L14-L35>, <https://github.com/persiyanov/herdr-reviewr/blob/859dd5b11a5280fe282de56b52378b20a54fa10a/.github/workflows/release.yml#L61-L63>
- plannotator/herdr-annotate: <https://github.com/plannotator/herdr-annotate/blob/c5e565e9bdbb52d152ad15169f38ed8bae31b610/scripts/fetch-herdr-annotate.sh>
- nicosuave/memex: <https://github.com/nicosuave/memex/blob/2d9e16f3b3e154d3f919c0a987bb09e4ad8fc7db/herdr/install.sh>
- ChmaraX/herdr-nvim: <https://github.com/ChmaraX/herdr-nvim/blob/d2dd1899dd7b4ee36719e315e97f326b8aada171/herdr/install.sh>, <https://github.com/ChmaraX/herdr-nvim/blob/d2dd1899dd7b4ee36719e315e97f326b8aada171/release-please-config.json>
- kryptamine/herdr-auto-title: <https://github.com/kryptamine/herdr-auto-title/blob/8b2504514bb2aaa79ecbba6cb0e6c426652f4419/herdr-plugin.toml>, <https://github.com/kryptamine/herdr-auto-title/blob/8b2504514bb2aaa79ecbba6cb0e6c426652f4419/release-please-config.json>
- permgps/herdr-telegram-agents: <https://github.com/permgps/herdr-telegram-agents/blob/11201cb256ff87a0a8102ce9bda2455e2b3638a1/scripts/install.sh>, <https://github.com/permgps/herdr-telegram-agents/blob/11201cb256ff87a0a8102ce9bda2455e2b3638a1/.goreleaser.yaml>, <https://github.com/permgps/herdr-telegram-agents/blob/11201cb256ff87a0a8102ce9bda2455e2b3638a1/.github/workflows/release.yml>
- aorumbayev/herdr-workflows: <https://github.com/aorumbayev/herdr-workflows/blob/140de99b7fa43b10dc2a294e0667c62fa2f763dd/.github/workflows/release.yml#L35-L106>
- 0xGosu/herdr-auto-pilot: <https://github.com/0xGosu/herdr-auto-pilot/blob/f769dd88797ef471e3bd01342aa509af81e4b13d/.github/workflows/auto-release.yml#L3-L21>
- m-mohamed/sheprd: <https://github.com/m-mohamed/sheprd/blob/2212eda029f202792eb331911ba5df2bc16807d9/scripts/install-plugin.sh#L110-L116>
- ardasevinc/herdr-codex-bridge: <https://github.com/ardasevinc/herdr-codex-bridge/blob/3a9c68c5e251c81757cc9e8f39fd3ec28f774011/scripts/install-plugin-worker.sh#L17-L38>
- joshuadavidthomas/hrd: <https://github.com/joshuadavidthomas/hrd/blob/0e6cb93added1ea087b7bb5c2b6b136b359fe464/install.sh#L45-L61>
- jtnovellis/herdr-worktree-setup: <https://github.com/jtnovellis/herdr-worktree-setup/blob/3da48c1b7e2454e5aaf1c72185e720938dd1637a/scripts/fetch-or-build.sh#L9-L15>
- quaywin/agys: <https://github.com/quaywin/agys/blob/d5485b794f4189308b4a674ab062da392f995026/herdr-plugin.toml#L9>
- RanolP/herdr-handsfree: <https://github.com/RanolP/herdr-handsfree/blob/61ee20b724ac2b17d63998a41ae9960e4361e7e3/herdr-plugin.toml#L11>
- openclaw/crabbox: <https://github.com/openclaw/crabbox/blob/186fd3382804a18d1eb7114df81c7a72faade97b/plugins/herdr/build.sh>
- furkankly/zoetrope: <https://github.com/furkankly/zoetrope/blob/b1f31dd26bd4e9e513885e39edb78d0850a5d1fe/herdr-plugin/herdr/ensure-zoe.sh>
- eugenioenko/ttt: <https://github.com/eugenioenko/ttt/blob/119553cc7615371489b87ed6d22e1490e7c0701c/herdr-plugin/scripts/install.sh>
- bon5co/bermuda: <https://github.com/bon5co/bermuda/blob/19e2679e1bb5d82a9ce2e86c81a2da2a78197126/herdr-plugin.toml>
- uwuclxdy/clauth: <https://github.com/uwuclxdy/clauth/blob/c6be6ecb0bddbe12e839a7c0f85b584fa61e5aa6/herdr-plugin/herdr-plugin.toml>
- pinkpixel-dev/quota: <https://github.com/pinkpixel-dev/quota/blob/9aeb7d8e206d857ee6f5a15eed6a16fdc714cb12/herdr-plugin/herdr-plugin.toml>
- Davidcreador/herdr-token-dashboard: <https://github.com/Davidcreador/herdr-token-dashboard/blob/03872156075a50125aa5adffdd485d89fe965dfa/scripts/build.sh>

Other ecosystems:

- zellij plugin loading: <https://github.com/zellij-org/zellij-org.github.io/blob/111f441276770f871419949a6695bbad6b518551/docs/src/plugin-loading.md>; permissions: <https://github.com/zellij-org/zellij-org.github.io/blob/111f441276770f871419949a6695bbad6b518551/docs/src/plugin-api-permissions.md>
- blink.cmp: <https://github.com/Saghen/blink.cmp/blob/v1.10.2/doc/configuration/fuzzy.md>, <https://github.com/Saghen/blink.cmp/blob/v1.10.2/lua/blink/cmp/fuzzy/download/init.lua>, <https://github.com/Saghen/blink.cmp/blob/v1.10.2/lua/blink/cmp/fuzzy/download/git.lua>, <https://github.com/Saghen/blink.cmp/blob/v1.10.2/lua/blink/cmp/fuzzy/download/files.lua>
- telescope-fzf-native.nvim: <https://github.com/nvim-telescope/telescope-fzf-native.nvim/blob/b25b749b9db64d375d782094e2b9dce53ad53a40/README.md#L32-L80>
- TPM: <https://github.com/tmux-plugins/tpm/blob/e261deb1b47614eed3400089ce7197dc68acc4eb/docs/how_to_create_plugin.md>; tmux-thumbs: <https://github.com/fcsonline/tmux-thumbs/blob/ae91d5f7c0d989933e86409833c46a1eca521b6a/tmux-thumbs-install.sh#L58-L104>

GitHub:

- Release access ("Anyone with read access…"): <https://github.com/github/docs/blob/dae83f7d28f3b8e10015bd6b6d8b72267cedceca/content/repositories/releasing-projects-on-github/about-releases.md>
- Attestation availability: <https://github.com/github/docs/blob/dae83f7d28f3b8e10015bd6b6d8b72267cedceca/data/reusables/gated-features/attestations.md>
- Go module authentication via `go.sum`: <https://go.dev/ref/mod#authenticating>
