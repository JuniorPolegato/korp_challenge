.PHONY: up down logs test provision load clean teardown reinstall

up:            ## Build and start the stack
	docker compose up -d --build

down:          ## Stop and remove the stack
	docker compose down

logs:          ## Follow all container logs
	docker compose logs -f

test:          ## Hit the endpoint through NGINX
	curl -s http://localhost:80/projeto-korp | jq .

load:          ## Generate traffic so the dashboard has data
	for i in $$(seq 1 500); do curl -s -o /dev/null http://localhost/projeto-korp; done

provision:     ## Provision everything with a single Ansible command
	cd ansible && ansible-playbook site.yml --ask-become-pass

clean:         ## Remove containers, volumes and the built image
	docker compose down -v --rmi local

teardown: ## Full wipe: containers, volumes, network, image and /opt/projeto-korp
	@bash scripts/teardown.sh

reinstall: teardown ## Teardown + provision from scratch
	cd ansible && ansible-playbook site.yml --ask-become-pass
