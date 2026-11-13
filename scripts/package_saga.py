#!/usr/bin/env python3
"""Create one reproducible source-only distribution without Git or secrets."""
import hashlib
from pathlib import Path
import zipfile

ROOT = Path(__file__).resolve().parents[1]
DIRECTORIES = ("internal", "order-service", "payment-service", "stock-service", "monolith",
               "gateway", "frontend", "infra", "cmd", "docs", "scripts")
ROOT_FILES = ("go.mod", "go.sum", "README.md", "SEMINAR_GUIDE.md", "Makefile", "env.example",
              ".gitignore", ".dockerignore", "docker-compose.yaml", "compose.saga.yaml", "Dockerfile.saga")
EXTENSIONS = {".go", ".sum", ".mod", ".md", ".yaml", ".yml", ".json", ".sql", ".py", ".txt",
              ".jsx", ".js", ".css", ".html", ".svg", ".conf", ".sh", ".proto"}
EXCLUDED = {".git", "node_modules", "dist", "__pycache__", ".aws", ".agents", ".codex"}


def source_files():
    files = [ROOT / name for name in ROOT_FILES]
    for directory in DIRECTORIES:
        files.extend(p for p in (ROOT / directory).rglob("*")
                     if p.is_file() and not p.is_symlink() and not EXCLUDED.intersection(p.relative_to(ROOT).parts)
                     and (p.suffix in EXTENSIONS or p.name in ("Dockerfile", ".dockerignore")))
    return sorted(files, key=lambda p: str(p.relative_to(ROOT)))


def build():
    output = ROOT / "dist"
    output.mkdir(exist_ok=True)
    files = source_files()
    name = "godelivery-saga"
    archive = output / (name + ".zip")
    with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as zipped:
        for path in files:
            relative = path.relative_to(ROOT).as_posix()
            info = zipfile.ZipInfo(name + "/" + relative, date_time=(2026, 1, 1, 0, 0, 0))
            info.create_system = 3
            info.external_attr = 0o100644 << 16
            info.compress_type = zipfile.ZIP_DEFLATED
            zipped.writestr(info, path.read_bytes(), compresslevel=9)
    print(f"{archive.relative_to(ROOT)} sha256={hashlib.sha256(archive.read_bytes()).hexdigest()}")


if __name__ == "__main__":
    build()
