from __future__ import annotations

import argparse

from tamoss.db.migrations.runner import migrate


def main() -> None:
    parser = argparse.ArgumentParser(prog="tamoss-db")
    subcommands = parser.add_subparsers(dest="command", required=True)

    migrate_command = subcommands.add_parser("migrate")
    migrate_command.add_argument("--revision", default="head")
    migrate_command.add_argument("--apply-fixtures", action="store_true")
    migrate_command.add_argument("--apply-cnpg-ownership", action="store_true")

    args = parser.parse_args()
    if args.command == "migrate":
        migrate(
            revision=args.revision,
            apply_fixtures=args.apply_fixtures,
            apply_cnpg_ownership=args.apply_cnpg_ownership,
        )
