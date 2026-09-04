# Support

Use GitHub Issues for public bug reports, feature requests, and operational
questions.

For informal questions and community chat, join the `#tamoss` channel in the
[Cloud Native Agile Production Slack](https://join.slack.com/t/cnap-media/shared_invite/zt-46so866cy-YstgGBzIgcA6U1c3J1Rg1g).

Before opening an issue, check:

- [README](README.md) for the supported deployment and development paths
- [Deployment](docs/deployment.md) for Kubernetes installation guidance
- [Troubleshooting](docs/troubleshooting.md) for common runtime issues
- [Security](SECURITY.md) for vulnerability reporting

Security vulnerabilities should not be reported in public issues. Follow the
private disclosure process in [SECURITY.md](SECURITY.md).

When opening an operational issue, include:

- TAMOSS version or image tags, if known.
- The selected profile: `local-kind`, `single-server`, or `multi-server`.
- The `Tamoss` status and condition summary.
- Relevant Kubernetes Events and pod log excerpts.
- Which providers are managed by TAMOSS and which are external.

Do not include Secret values, bearer tokens, OAuth client secrets, private keys,
database passwords, S3 access keys, or complete presigned object URLs. Redact
hostnames if they are not public.

Maintainers triage public issues on a best-effort basis. If you have a separate
commercial support agreement, use that channel for urgent production incidents;
otherwise use GitHub Issues, GitHub Discussions, or the `#tamoss` Slack channel.
