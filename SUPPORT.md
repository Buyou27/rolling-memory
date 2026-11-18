# Support

**[English](SUPPORT.md) · [简体中文](docs/zh-CN/SUPPORT.md)**

## Where to get help

- **Official website**: <https://rolling-memory.rocareer.com/>.
- **Documentation**: [docs/](docs/README.md) and the in-app help center.
- **Email support**: `support@psychologylake.org.cn` (organization mailbox).
- **Cloud capability application (advanced & enterprise), partnership & investment**: `pick@psychologylake.org.cn`.
- **Bug reports and feature requests**: open an issue at
  <https://github.com/rocareer/rolling-memory/issues>.
- **Security reports**: follow [SECURITY.md](SECURITY.md); do not open a public
  issue for a vulnerability.
- **Questions and usage discussion**: use GitHub Discussions when available, or
  the issue tracker with the `question` label.

## Response expectations

This is a community-maintained open-source project. Support is best-effort and
provided without a service-level agreement (SLA). We aim to triage new issues
within a few business days, but response times vary with maintainer
availability.

## Languages

English and Simplified Chinese are supported for issues and documentation.
Other languages are welcome but may receive a slower response.

## Supported versions

The latest released minor version receives fixes. Older minor versions may be
asked to upgrade before a fix is provided. The local product needs no account or
subscription and makes no memory-plane network requests. The separate cloud
memory service is application-based ([preview](docs/privacy.md)) and is not
covered by this community support policy.

## Scope

We help with:

- installation and host integration (MCP, CLI, plugin installers);
- local-mode import, recall, export, backup, and migration;
- reproducible bugs in the released source or binaries;
- documentation errors.

We do not provide:

- custom development, private deployments, or consulting;
- guarantees about third-party hosts, models, or cloud providers;
- support for modified forks under the official project name.

## Before opening an issue

1. Run `rolling-memory doctor --json` and include the output with secrets
   removed.
2. State the version (`rolling-memory --version`), operating system, and host
   integration.
3. Provide a minimal reproduction and the exact command you ran.
4. Remove API keys, tokens, personal data, and vault contents from logs.
