# Production

`multi-server` is the production reference profile for self-managed Kubernetes.
Production guidance is split by operational concern:

- [Multi Server](getting-started/multi-server.md)
- [Provider Ownership](concepts/provider-ownership.md)
- [Backup and Restore](operations/backup-restore.md)
- [Upgrades](operations/upgrades.md)
- [Deletion Protection](operations/deletion-protection.md)
- [Day 2 Operations](operations/day-2.md)
- [Troubleshooting](operations/troubleshooting.md)

Before accepting durable data, confirm restore has been tested for the selected
PostgreSQL and S3 providers. For managed CNPG PostgreSQL, also confirm the
`BackupPolicyReady` condition is ready and a CNPG `ScheduledBackup` exists.
