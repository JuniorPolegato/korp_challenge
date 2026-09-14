#!/usr/bin/env bash
set -euo pipefail

sudo docker compose -p projeto-korp -f /opt/projeto-korp/docker-compose.yml \
  down -v --rmi local --remove-orphans 2>/dev/null || true
sudo docker network rm korp-net 2>/dev/null || true
sudo docker volume rm korp-prometheus-data korp-grafana-data 2>/dev/null || true
sudo docker rmi korp/http-server-projeto-korp:1.0.0 2>/dev/null || true
sudo rm -rf /opt/projeto-korp
echo "Teardown complete. Port 80:"
sudo ss -lptn 'sport = :80' || echo "  free"
