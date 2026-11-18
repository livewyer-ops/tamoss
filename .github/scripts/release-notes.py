"""Print the reviewed changelog entry for an exact release version."""

import re
import sys
from pathlib import Path

version = sys.argv[1]
changelog = Path("CHANGELOG.md").read_text(encoding="utf-8")
for section in re.split(r"^## ", changelog, flags=re.MULTILINE)[1:]:
    heading, _, body = section.partition("\n")
    if heading.split(" - ", 1)[0] == version and body.strip():
        print(body.strip())
        break
else:
    sys.exit(f"Missing or empty changelog entry for {version}")
