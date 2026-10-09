#!/usr/bin/env python3
"""Freeze argv from the preserved Go help examples; never execute the examples."""
import json
from pathlib import Path
import shlex

fixtures = Path(__file__).resolve().parent.parent / "tests" / "fixtures"
examples = []


def walk(command):
    for line in command.get("examples", "").replace("\\\n", " ").splitlines():
        for segment in line.strip().split(" | "):
            segment = segment.strip()
            if not segment.startswith("ob "):
                continue
            segment = segment.split(" > ", 1)[0]
            examples.append({"source_path": command["path"], "args": shlex.split(segment)[1:]})
    for child in command.get("children") or []:
        walk(child)


walk(json.loads((fixtures / "go-surface.json").read_text()))
(fixtures / "command-examples.json").write_text(json.dumps(examples, indent=2) + "\n")
print(f"Extracted {len(examples)} example command lines without running them.")
