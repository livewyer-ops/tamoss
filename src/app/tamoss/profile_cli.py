from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any
from uuid import UUID

from tamoss.adapters.postgres import PostgresRepository
from tamoss.application.contexts.profiles import (
    ProfileConflict,
    ProfileInUse,
    ProfileUseCases,
    profile_payload,
)
from tamoss.auth import Identity
from tamoss.domain.model import StorageBackend
from tamoss.errors import BadRequest, NotFound
from tamoss.settings import get_settings

EXIT_INVALID = 2
EXIT_CONFLICT = 3
EXIT_IN_USE = 4


def main() -> None:
    parser = argparse.ArgumentParser(prog="tamoss-profile")
    commands = parser.add_subparsers(dest="command", required=True)

    ensure = commands.add_parser("ensure")
    ensure.add_argument("--file", type=Path, required=True)
    ensure.add_argument("--created-by", required=True)

    delete = commands.add_parser("delete-if-unused")
    delete.add_argument("--id", type=UUID, required=True)

    args = parser.parse_args()
    try:
        _run(args)
    except (BadRequest, NotFound, TypeError, ValueError) as exc:
        _fail(EXIT_INVALID, str(exc))
    except ProfileConflict as exc:
        _fail(EXIT_CONFLICT, str(exc))
    except ProfileInUse as exc:
        _fail(EXIT_IN_USE, str(exc))


def _run(args: argparse.Namespace) -> None:
    repository = _repository()
    try:
        profiles = ProfileUseCases(repository=repository.profile_repository)
        if args.command == "ensure":
            payload = _profile_document(args.file)
            profile_id = UUID(str(payload.get("id")))
            profile, created = profiles.ensure_profile(
                profile_id=profile_id,
                payload=payload,
                identity=Identity(
                    subject=args.created_by,
                    method="operator",
                    scopes=frozenset({"admin"}),
                ),
            )
            print(
                json.dumps(
                    {
                        "action": "created" if created else "adopted",
                        "profile": profile_payload(profile),
                    },
                    separators=(",", ":"),
                    sort_keys=True,
                )
            )
            return

        deleted = profiles.delete_profile_if_unused(args.id)
        print(
            json.dumps(
                {"action": "deleted" if deleted else "absent", "id": str(args.id)},
                separators=(",", ":"),
                sort_keys=True,
            )
        )
    finally:
        repository.close()


def _profile_document(path: Path) -> dict[str, Any]:
    payload = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(payload, dict):
        raise ValueError("Profile document must be a JSON object")
    return payload


def _repository() -> PostgresRepository:
    settings = get_settings()
    database_url = settings.database_url_value()
    if database_url is None:
        raise RuntimeError("POSTGRES_HOST is required")
    placeholder = StorageBackend(
        id=UUID(int=0),
        label="operator-profile-registration",
        provider="operator",
        region="internal",
        store_product="none",
    )
    return PostgresRepository(
        database_url=database_url,
        database_url_provider=settings.database_url_value,
        storage_backend=placeholder,
        pool_min_size=1,
        pool_max_size=1,
    )


def _fail(code: int, message: str) -> None:
    print(message, file=sys.stderr)
    raise SystemExit(code)
