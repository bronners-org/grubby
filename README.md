# Grubby

[![Go Report Card](https://goreportcard.com/badge/github.com/simonbronner/grubby)](https://goreportcard.com/report/github.com/simonbronner/grubby)
[![Go Version](https://img.shields.io/badge/go-1.21%2B-blue.svg)](https://golang.org)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

**Grubby** is a high-performance Terminal User Interface (TUI) cockpit for developers interacting with GitHub across multiple repositories, squads, and local workspaces.

It merges **remote GitHub state** (pull requests, unresolved review threads, team activity, vulnerability alerts) with **local developer reality** (cloned workspaces, branch status, ahead/behind tracking, and instant cross-repo code search).

---

## Features

### 1. Repositories Cockpit & Consolidated Pull Requests
* **Workspace Status Awareness**: Scans your local workspace folder (default `~/workspaces/*`) and classifies branches non-destructively: `[✓ synced]`, `[↓ behind]`, `[↑ ahead]`, `[* dirty]`, or `[Not Cloned]`. When a repository is cloned via `c`, Grubby immediately detects the clone and reflects its git status and local path without requiring a manual refresh.
* **Top-Level Session Switcher (`s`)**: Press `s` at the top level on any repository or pull request row to instantly jump into or spawn its dedicated tmux session.
* **Sorting Leader (`S`)**:
  * `S` → `v`: Sort by vulnerabilities (most to least).
  * `S` → `p`: Sort by open pull requests (most to least) and auto-expand repositories with open PRs.
  * `S` → `l`: Sort by last updated / pushed timestamp.
  * `Esc`: Instantly reset to default tier-grouped sort order.
* **Filtering Leader (`f`)**:
  * `f` → `n`: Real-time substring filter on repository name.
  * `f` → `l`: Filter repositories by defined group / label.
  * `f` → `s`: Toggle squad ownership filter.
* **Inline Pull Request Tree & Actions**:
  * `Enter` / `Space` expands or collapses open pull requests under any repository.
  * **When a Pull Request row is selected**:
    * `m`: Merge approved pull requests directly (requires `APPROVED`).
    * `p`: Trigger GitHub Copilot PR code review.
    * `k` or `c`: Checkout PR branch into local workspace.
    * `s`: Launch dedicated tmux session for the repository.
    * `t`: Create review task for squad or member in Tasks.
    * `Enter` / `o`: Open pull request in browser.
    * `y`: Copy pull request URL to clipboard.
    * `Left` / `h`: Jump back to parent repository row.
* **Developer Ergonomics**:
  * `c` clones uncloned repositories into `~/workspaces/{repo}` using `gh repo clone`.
  * `o` in-place leader key to open GitHub tabs (Web overview, Editor, Session, PRs, Issues, Actions, Security, Releases, Settings).
  * `y` yank leader key to copy URLs / session name to clipboard.
  * `C` claims or toggles repository ownership for the active squad.
  * `l` opens the Labels modal to manage repository tags.

### 2. Consolidated Tasks Dashboard
* **Unified Workspace & Squad Tasks**: Displays your personal tasks (backed by your personal repository) alongside squad tasks (backed by the home organization `grubby-tasks`).
* **Ergonomic Leader Keys**:
  * `a` (Add Task leader): prompts for `[m]y task` or `[s]quad task` inline creation.
  * `g` (Go to Task leader): overlays visible numeric labels on tasks for instant jump navigation (`[g]` → `[#]` + `Enter`), with `[f]irst` and `[l]ast` shortcuts.
* **Smart Section Collapsing**: Completed tasks are collapsed by default and skipped during fast numeric jump navigation. Toggle completed section with `D`.
* **Issue & PR Integration**: PR review tasks link directly to the underlying pull request (`Enter`/`o` opens the PR, `i` opens the issue).

### 3. Squad Activity Feed
* **Watched Squad Focus**: Group squad members together and track what they are actively shipping across the organization (PR opens, merges, comments, commits).
* **Organization Member Selection**:
  * `a` opens the **Add Member Modal** with a searchable, scrollable list of organization members—choose any colleague with `Space` or `Enter` without having to manually look up their handles!
  * `s` opens the **Squad Switcher Modal** to pivot between watched squads. Press `+` or `n` to create a new squad/group and immediately pick members from the organization.
  * `d` removes members from the active squad.

### 4. Cross-Repo Code Search & Live Syntax Preview
* **Split-Pane Search Cockpit**:
  * **Left Pane (Interactive Tree)**: Hierarchical tree navigator (`Repository` → `File` → `Line Match`) navigable via `j`/`k`, with collapsible branches (`h`/`l`/`Space`).
  * **Right Pane (Chroma Syntax Preview)**: Live code preview powered by `alecthomas/chroma/v2` with line numbers and distinct matched line pointer (`▶ 42 │`).
* **Zero-Index Overhead**: Powered by `ripgrep --json` streaming under the hood. Queries across dozens of local repositories execute in under 80ms while respecting `.gitignore`.
* **Jump to Editor**: Press `Enter` on any match to open your editor directly at that file and line number.

### 5. Notifications & macOS Desktop Integration
* **Header Notification Bubble**: Dynamic visual indicator (`🔔 {n} unread`) tracking unread GitHub notifications in real time.
* **macOS Native Notification Banners**:
  * **GitHub Ingestion**: Automatically fires native macOS notification banners with chimes whenever a refresh discovers new unread notifications.
  * **Window & Task Dispatch**: Triggers native desktop notification bubbles when opening dedicated browser tabs, launching Neovim in iTerm2, spawning shell sessions, or completing repository clones and fast-forwards.

### 6. Terminal-Responsive Dynamic Tables
* **Adaptive Column Sizing**: All views (Repositories, Pull Requests, Squad Activity, Security) compute layout widths dynamically from the terminal dimensions (`m.Width`), maximizing readability and eliminating awkward text wrapping or truncated paths on larger monitors.

### 7. Rate-Limit Friendly & Local-First
* **Batched GraphQL Queries**: Eliminates REST N+1 query bottlenecks by consolidating repositories, PRs, review threads, and CI checks into single GraphQL round-trips.
* **Automatic Credentials**: Re-uses your existing `gh auth token` session without requiring manual PAT setup.
* **Live Quota Monitoring**: Tracks your remaining API rate limit and reset window in the top status bar.

---

## Installation

### Homebrew (macOS & Linux)

The easiest way to install Grubby on macOS or Linux is via Homebrew:

```bash
# Install directly from the tap:
brew install simonbronner/tap/grubby

# Or tap first and install:
brew tap simonbronner/tap
brew install grubby
```

To upgrade in the future:
```bash
brew update
brew upgrade grubby
```

### Pre-built Binaries (GitHub Releases)

Download pre-compiled binaries from [GitHub Releases](https://github.com/simonbronner/grubby/releases):
* **macOS**: Apple Silicon (`darwin_arm64`) & Intel (`darwin_amd64`)
* **Linux**: x86_64 (`linux_amd64`) & ARM64 (`linux_arm64`)
* **Windows**: x86_64 (`windows_amd64`)

### Build from Source

#### Prerequisites
* **Go**: 1.21 or later
* **GitHub CLI (`gh`)**: Authenticated via `gh auth login` with `repo` and `read:org` scopes.
* **Ripgrep (`rg`)**: Recommended for real-time code search (`brew install ripgrep`).

```bash
git clone https://github.com/simonbronner/grubby.git
cd grubby
go build -o grubby ./cmd/grubby
```

To install directly to `$GOPATH/bin`:
```bash
go install ./cmd/grubby
```

---

## Quick Start

Launch Grubby with your default GitHub user / org:
```bash
grubby
```

Specify a target GitHub organization or custom workspace path:
```bash
grubby --org acme-corp --workspace ~/workspaces
```

### Corporate Proxy & Enterprise Environments
Grubby is fully equipped for enterprise networks and corporate proxies (Zscaler, Netskope, BlueCoat, Squid):

* **Automatic Detection**: Automatically checks shell proxy variables (`HTTPS_PROXY`, `HTTP_PROXY`, `ALL_PROXY`) and your git configuration (`git config --get https.proxy` or `http.proxy`).
* **Explicit Proxy**: Pass `--proxy <url>` or set `proxy: <url>` in `config.yaml`:
  ```bash
  grubby --proxy http://corporate-proxy.corp.internal:8080
  # SOCKS5 proxies are also supported:
  grubby --proxy socks5://127.0.0.1:1080
  ```
* **Corporate SSL Inspection (MITM)**: If your corporate proxy intercepts TLS traffic with internal root CAs:
  * Pass `--insecure` or set `insecure_skip_verify: true` in `config.yaml` to skip TLS verification.
  * Or specify your corporate root certificate bundle via `export SSL_CERT_FILE=/path/to/corporate-ca.pem`.
* **Child Process Propagation**: Any proxy or TLS settings resolved by Grubby are automatically passed down to spawned processes (such as `gh repo clone` or `git fetch`).
* **Resilient to 502 Bad Gateway**: Grubby decouples repository metadata from open pull requests using indexed search queries, eliminating GitHub's 10-second backend timeout on large corporate organizations, and automatically retries transient gateway 502/503/504 errors with exponential backoff.

---

## Keyboard Shortcuts

Press `?` at any time inside Grubby to display the contextual keybindings overlay.

### Global
| Key | Action |
| :--- | :--- |
| `1` – `5` or `Tab` | Switch between primary tabs |
| `r` | Refresh data from GitHub |
| `?` | Toggle help dialog overlay |
| `q` or `Ctrl+C` | Quit |

### Tab 1: Repositories
| Key | Action |
| :--- | :--- |
| `j` / `k` or `↑` / `↓` | Select repository |
| `g` | Open **Manage Groups Modal** (Space to toggle, `+` to add new group) |
| `c` | Clone repository to `~/workspaces/{repo}` |
| `e` | Open local repository in `$EDITOR` |
| `t` | Open repository in a new shell (iTerm2 on macOS) |
| `o` | Open repository in web browser |
| `S` | Open repository Security (Dependabot) alerts directly in browser |
| `u` | Safe fast-forward clean `behind` repository |

### Tab 2: Pull Requests
| Key | Action |
| :--- | :--- |
| `j` / `k` or `↑` / `↓` | Select pull request |
| `PgDn` / `PgUp` (or `Ctrl+D/U`) | Page down / up through pull requests |
| `Home` / `End` (or `g` / `G`) | Jump to top / bottom |
| `f` | Filter pull requests by title, repo, or author |
| `Esc` | Clear pull request filter |
| `o` | Open pull request in browser |
| `p` | Trigger GitHub Copilot code review |
| `c` | Checkout pull request branch in local workspace |

### Tab 3: Squad Activity
| Key | Action |
| :--- | :--- |
| `s` | Open **Squad Switcher Modal** |
| `n` or `+` | Create a new squad/group and pick members |
| `a` | Open **Add Member Modal** (pick from organization members list or search) |
| `d` | Remove member from watched squad |
| `o` | Open activity target in browser |

### Tab 4: Code Search
| Key | Action |
| :--- | :--- |
| `/` | Focus search input box |
| `j` / `k` or `↑` / `↓` | Navigate results tree |
| `h` / `l` or `Space` | Collapse / expand repository or file branch |
| `Tab` | Switch focus between Tree Navigator and Code Preview |
| `Enter` | Open selected match in `$EDITOR` at exact line number |

### Tab 5: Notifications
| Key | Action |
| :--- | :--- |
| `j` / `k` or `↑` / `↓` | Select notification thread |
| `Enter` or `o` | Open notification in dedicated browser and mark as read |
| `m` | Mark selected notification as read |
| `M` | Mark all notifications as read |

---

## Configuration (`~/.config/grubby/config.yaml`)

Grubby automatically generates and maintains `~/.config/grubby/config.yaml`. Actions performed in the TUI (adding groups, adding squad members, switching watched squads) write back to this file atomically:

```yaml
version: 1
default_org: "acme-corp"
workspace_dir: "~/workspaces"
editor: "nvim" # or "code", fallback to $EDITOR

# Optional Corporate Network Settings
proxy: "http://proxy.corp.internal:8080" # or socks5://127.0.0.1:1080
insecure_skip_verify: false              # set to true if corporate proxy performs SSL interception

active_squad: "checkout-squad"

squads:
  - name: "checkout-squad"
    display_name: "Checkout Squad"
    members:
      - "alice"
      - "bob"
      - "charlie"

groups:
  - name: "payments"
    display_name: "Payments Solution"
    repos:
      - "api-payments"
      - "api-billing"

search:
  engine: "ripgrep"
  ignore_dirs:
    - ".git"
    - "node_modules"
    - "vendor"
    - "dist"
```

---

## Architecture & Technology Stack

* **TUI Framework**: Built on the [Charm](https://charm.sh) ecosystem using [Bubble Tea](https://github.com/charmbracelet/bubbletea) (The Elm Architecture in Go) and [Lipgloss](https://github.com/charmbracelet/lipgloss).
* **Syntax Highlighting**: [Chroma v2](https://github.com/alecthomas/chroma) (pure Go syntax engine with Dracula styling).
* **Search Engine**: Parallel streaming `ripgrep` runner parsing structured JSON Lines.
* **Git Engine**: Safe non-destructive status inspection and fast-forwarding (`git rev-list`, `git merge --ff-only`).

---

## Releasing & Multi-Platform Automation

Grubby includes fully automated release workflows for multi-platform builds, GitHub Releases, and Homebrew tap publishing.

### 1. Tagging a Release
Use the release helper script to automatically increment the next version tag (`v1`, `v2`, `v3`, etc.) and push it to GitHub:

```bash
# Auto-detects the next release tag (e.g. v1 -> v2) and prompts for confirmation
./scripts/release.sh

# Or trigger a specific tag directly:
./scripts/release.sh v1
```

### 2. GitHub Actions Automation
When a `v*` tag is pushed:
1. GitHub Actions triggers `.github/workflows/release.yml`.
2. Compiles cross-platform binaries for Darwin (`amd64`, `arm64`), Linux (`amd64`, `arm64`), and Windows (`amd64`).
3. Packages `.tar.gz` and `.zip` archives and computes SHA256 checksums (`checksums.txt`).
4. Generates a matching `Formula/grubby.rb` with the archive checksums.
5. Creates the GitHub Release with attached binaries, checksums, and auto-generated release notes.
6. Synchronizes the Homebrew tap repository [`simonbronner/homebrew-tap`](https://github.com/simonbronner/homebrew-tap).

### 3. Homebrew Tap Synchronization
To sync or update the tap formula locally at any time:
```bash
./scripts/update-tap.sh v1
```

---

## License

[MIT](LICENSE)
