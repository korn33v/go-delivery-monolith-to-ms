#!/usr/bin/env python3
"""Run the four original migration stages without changing the user's .env."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import uuid

from package_saga import ROOT, source_files


def run():
    output = ROOT / "dist" / "strangler-regression.log"
    output.parent.mkdir(exist_ok=True)
    saga = ["docker", "compose", "-f", str(ROOT / "compose.saga.yaml")]
    running = subprocess.run(saga + ["ps", "--status", "running", "-q"], cwd=ROOT,
                             check=True, text=True, capture_output=True).stdout.strip()
    with output.open("w") as log, tempfile.TemporaryDirectory(prefix="godelivery-regression-") as directory:
        work = Path(directory)
        for source in source_files():
            target = work / source.relative_to(ROOT)
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source, target)
        shutil.copy2(work / "env.example", work / ".env")
        environment = {
            "SAGA_ENABLED": "false", "OTEL_ENABLED": "true", "KAFKA_BROKERS": "kafka:9092",
            "MONOLITH_URL": "http://monolith:8081", "STOCK_URL": "http://stock-service:8082",
            "OTEL_EXPORTER_OTLP_ENDPOINT": "http://jaeger:4318",
        }
        services = {
            "monolith": {"environment": {"STOCK_URL": "http://stock-service:8082"}},
            "gateway": {"environment": {"STOCK_URL": "http://stock-service:8082", "ORDER_URL": "http://order-service:8083"}},
        }
        for name, port in (("stock-service", 8082), ("payment-service", 8084), ("order-service", 8083)):
            services[name] = {
                "build": {"context": ".", "dockerfile": "Dockerfile.saga", "args": {"SERVICE": name}},
                "env_file": ".env", "environment": environment,
                "ports": [f"127.0.0.1:{port}:{port}"],
                "depends_on": {"monolith": {"condition": "service_healthy"}},
                "healthcheck": {"test": ["CMD", "wget", "-q", "--spider", f"http://127.0.0.1:{port}/health"],
                                "interval": "2s", "timeout": "3s", "retries": 30},
            }
        override = work / "regression.compose.json"
        override.write_text(json.dumps({"services": services}))
        compose = ["docker", "compose", "-p", "godelivery-regression-" + uuid.uuid4().hex[:8],
                   "-f", "docker-compose.yaml", "-f", override.name]
        host_env = dict(os.environ, ENV_FILE=str(work / ".env"))
        # The copy has its own flags and Kafka. Original Saga volumes are retained.
        def command(args):
            subprocess.run(args, cwd=work, env=host_env, stdout=log, stderr=subprocess.STDOUT,
                           text=True, timeout=600, check=True)
        try:
            if running:
                print("Temporarily stopping Saga; its SQL/Kafka volumes are retained", flush=True)
                subprocess.run(saga + ["down"], cwd=ROOT, stdout=log, stderr=subprocess.STDOUT,
                               timeout=120, check=True)
            command(compose + ["up", "-d", "--build", "--wait", "--wait-timeout", "180", "monolith", "gateway", "frontend"])
            for stage, service in ((1, None), (2, "stock-service"), (3, "payment-service"), (4, "order-service")):
                if service:
                    # Live flags changed in .env. Recreating the already running
                    # in-memory monolith would reset its inventory; host-based
                    # seminars likewise start only the newly extracted service.
                    command(compose + ["up", "-d", "--no-deps", "--build", "--wait", "--wait-timeout", "120", service])
                    command(["go", "run", "./cmd/lab", str(stage)])
                command(["go", "run", "./cmd/smoke", f"-stage={stage}", "-require-trace"])
                print(f"SUCCESS Strangler stage {stage}: paid once, stock consistent, end-to-end Jaeger trace", flush=True)
        finally:
            subprocess.run(compose + ["down", "-v"], cwd=work, stdout=log, stderr=subprocess.STDOUT,
                           timeout=120, check=True)
            if running:
                subprocess.run(saga + ["up", "-d", "--wait", "--wait-timeout", "180"], cwd=ROOT,
                               stdout=log, stderr=subprocess.STDOUT, timeout=240, check=True)
    print(f"SUCCESS Strangler regression; log: {output}", flush=True)


if __name__ == "__main__":
    try:
        run()
    except (OSError, subprocess.SubprocessError) as error:
        print(f"FAILURE Strangler regression: {error}", file=sys.stderr)
        log = ROOT / "dist" / "strangler-regression.log"
        if log.exists():
            print("\n".join(log.read_text()[-12000:].splitlines()[-80:]), file=sys.stderr)
        sys.exit(1)
