BINARY       := akb48
VPS_USER     := dandi
VPS_HOST     := $(VPS_HOST)
VPS_DIR      := /home/dandi/akb48
COMPOSE      := docker compose -f deploy/docker-compose.yml

.PHONY: build test validate deploy logs restart setup-vps \
        docker-build docker-up docker-up-all docker-down docker-logs docker-shell

build:
	go build -o $(BINARY) ./cmd/akb48/

test:
	go test ./... -timeout 120s

validate: build
	ANTHROPIC_API_KEY=test ./$(BINARY) --validate

deploy: build test
	rsync -avz $(BINARY) config.toml identity/ skills/ $(VPS_USER)@$(VPS_HOST):$(VPS_DIR)/
	ssh $(VPS_USER)@$(VPS_HOST) "sudo systemctl restart akb48"
	@echo "Deployed. Check: ssh $(VPS_USER)@$(VPS_HOST) 'journalctl -u akb48 -f'"

logs:
	ssh $(VPS_USER)@$(VPS_HOST) "journalctl -u akb48 -f --no-pager"

restart:
	ssh $(VPS_USER)@$(VPS_HOST) "sudo systemctl restart akb48"

setup-vps:
	ssh $(VPS_USER)@$(VPS_HOST) "mkdir -p $(VPS_DIR)/sessions $(VPS_DIR)/logs"
	rsync -avz deploy/akb48.service $(VPS_USER)@$(VPS_HOST):/etc/systemd/system/
	ssh $(VPS_USER)@$(VPS_HOST) "sudo systemctl daemon-reload && sudo systemctl enable akb48"

# --- Docker (local) ---

docker-build:
	$(COMPOSE) build akb48

docker-up:
	$(COMPOSE) up akb48

docker-up-all:
	$(COMPOSE) up

docker-down:
	$(COMPOSE) down

docker-logs:
	$(COMPOSE) logs -f akb48

docker-shell:
	$(COMPOSE) exec akb48 sh
