#!/usr/bin/env python3
"""Finalize Archify candidates sequentially, retaining each browser receipt."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[1]
NAMES = ("saga-containers", "saga-components", "saga-sequence", "saga-erd-orders", "saga-erd-payments", "saga-erd-stock")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--only", nargs="+", choices=NAMES)
    args = parser.parse_args()
    cli = Path(os.getenv("ARCHIFY_CLI", str(Path.home() / ".codex/skills/archify/bin/archify.mjs")))
    if not cli.is_file():
        raise FileNotFoundError("Install Archify v3.2.0 or set ARCHIFY_CLI to bin/archify.mjs")
    for name in args.only or NAMES:
        candidate = ROOT / "docs/sources" / (name + ".json")
        data = json.loads(candidate.read_text())
        # Every later browser run owns a fresh evidence directory.
        evidence = ROOT / "docs/verification" / name / ("run-" + str(time.time_ns()))
        result = subprocess.run(["node", str(cli), "finalize", data["diagram_type"], str(candidate),
                                 data["meta"]["output"], "--repo-root", str(ROOT), "--quality", data["meta"]["quality_profile"],
                                 "--out-dir", str(evidence), "--json"], cwd=ROOT, text=True,
                                capture_output=True, timeout=180)
        receipt = json.loads(result.stdout)
        print(json.dumps({"name": name, "ok": receipt["ok"], "gates": receipt.get("gates"),
                          "diagnostics": receipt.get("diagnostics"), "evidence": receipt.get("evidence"),
                          "visualReviewRecommendation": receipt.get("visualReviewRecommendation"),
                          "layoutReviewRecommendation": receipt.get("layoutReviewRecommendation"),
                          "update": receipt.get("update")}, ensure_ascii=False), flush=True)
        if result.returncode:
            return result.returncode
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        print(f"FAILURE Archify: {error}", file=sys.stderr)
        sys.exit(1)
