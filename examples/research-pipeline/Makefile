# Research-pipeline orchestration.
#
# `make up`   — start all worker agents, their sidecars, and the planner.
# `make down` — stop everything that `make up` started.
# `make logs` — tail every component's log.
# `make clean`— stop everything and wipe logs/pids.
#
# Activate your venv before running (the agents use the venv's Python).

PIDS_DIR := pids
LOGS_DIR := logs
SIDECAR  := ../../bin/sidecar
PY       := python

.PHONY: up down logs clean proto

proto:
	@./generate_proto.sh

up: $(PIDS_DIR) $(LOGS_DIR)
	@echo "Starting Python worker agents..."
	@PORT=8001 $(PY) agents/searcher.py     > $(LOGS_DIR)/searcher-1.log    2>&1 & echo $$! > $(PIDS_DIR)/searcher-1.pid
	@PORT=8002 $(PY) agents/searcher.py     > $(LOGS_DIR)/searcher-2.log    2>&1 & echo $$! > $(PIDS_DIR)/searcher-2.pid
	@PORT=8003 $(PY) agents/scraper.py      > $(LOGS_DIR)/scraper-1.log     2>&1 & echo $$! > $(PIDS_DIR)/scraper-1.pid
	@PORT=8004 $(PY) agents/summarizer.py   > $(LOGS_DIR)/summarizer-1.log  2>&1 & echo $$! > $(PIDS_DIR)/summarizer-1.pid
	@PORT=8005 $(PY) agents/summarizer.py   > $(LOGS_DIR)/summarizer-2.log  2>&1 & echo $$! > $(PIDS_DIR)/summarizer-2.pid
	@PORT=8006 $(PY) agents/synthesizer.py  > $(LOGS_DIR)/synthesizer-1.log 2>&1 & echo $$! > $(PIDS_DIR)/synthesizer-1.pid
	@sleep 2
	@echo "Starting sidecars (one per worker)..."
	@$(SIDECAR) --agent-id=searcher-1     --capabilities=search     --endpoint=localhost:9090 --listen-addr=:9090 --forward-to-url=http://localhost:8001/invoke > $(LOGS_DIR)/sidecar-searcher-1.log     2>&1 & echo $$! > $(PIDS_DIR)/sidecar-searcher-1.pid
	@$(SIDECAR) --agent-id=searcher-2     --capabilities=search     --endpoint=localhost:9092 --listen-addr=:9092 --forward-to-url=http://localhost:8002/invoke > $(LOGS_DIR)/sidecar-searcher-2.log     2>&1 & echo $$! > $(PIDS_DIR)/sidecar-searcher-2.pid
	@$(SIDECAR) --agent-id=scraper-1      --capabilities=scrape     --endpoint=localhost:9093 --listen-addr=:9093 --forward-to-url=http://localhost:8003/invoke > $(LOGS_DIR)/sidecar-scraper-1.log      2>&1 & echo $$! > $(PIDS_DIR)/sidecar-scraper-1.pid
	@$(SIDECAR) --agent-id=summarizer-1   --capabilities=summarize  --endpoint=localhost:9094 --listen-addr=:9094 --forward-to-url=http://localhost:8004/invoke > $(LOGS_DIR)/sidecar-summarizer-1.log   2>&1 & echo $$! > $(PIDS_DIR)/sidecar-summarizer-1.pid
	@$(SIDECAR) --agent-id=summarizer-2   --capabilities=summarize  --endpoint=localhost:9095 --listen-addr=:9095 --forward-to-url=http://localhost:8005/invoke > $(LOGS_DIR)/sidecar-summarizer-2.log   2>&1 & echo $$! > $(PIDS_DIR)/sidecar-summarizer-2.pid
	@$(SIDECAR) --agent-id=synthesizer-1  --capabilities=synthesize --endpoint=localhost:9096 --listen-addr=:9096 --forward-to-url=http://localhost:8006/invoke > $(LOGS_DIR)/sidecar-synthesizer-1.log  2>&1 & echo $$! > $(PIDS_DIR)/sidecar-synthesizer-1.pid
	@sleep 2
	@echo "Starting planner..."
	@PORT=8000 $(PY) agents/planner.py > $(LOGS_DIR)/planner.log 2>&1 & echo $$! > $(PIDS_DIR)/planner.pid
	@sleep 1
	@echo ""
	@echo "All processes started."
	@echo "  Research UI:  http://localhost:8000"
	@echo "  Mesh viz:     http://localhost:8082"
	@echo "  Jaeger:       http://localhost:16686"
	@echo ""
	@echo "Logs:  $(LOGS_DIR)/   PIDs:  $(PIDS_DIR)/"

down:
	@echo "Stopping all research-pipeline processes..."
	@for f in $(PIDS_DIR)/*.pid; do \
		[ -e "$$f" ] || continue; \
		pid=$$(cat "$$f"); \
		if kill -0 "$$pid" 2>/dev/null; then \
			kill -TERM "$$pid" 2>/dev/null || true; \
		fi; \
		rm -f "$$f"; \
	done
	@echo "Down."

logs:
	@tail -F $(LOGS_DIR)/*.log

clean: down
	@rm -rf $(LOGS_DIR) $(PIDS_DIR)
	@echo "Cleaned."

$(PIDS_DIR) $(LOGS_DIR):
	@mkdir -p $@
