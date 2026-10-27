"""Keep public CI independent of Kupe's private workflow repository."""
from pathlib import Path
import re
import sys

root = Path(__file__).resolve().parents[1]
errors = []
for path in (root / ".github/workflows").glob("*.y*ml"):
    text = path.read_text()
    if "kupecloud/github-workflows" in text:
        errors.append(f"{path.relative_to(root)}: private workflow/config dependency")
    for target in re.findall(r"(?m)^\s*uses:\s*(\./\.github/workflows/[^\s#]+)", text):
        if not (root / target).is_file():
            errors.append(f"{path.relative_to(root)}: missing local workflow {target}")
if errors:
    print("\n".join(errors), file=sys.stderr)
    sys.exit(1)
print("Public CI uses repository-local Kupe workflows/config only")
