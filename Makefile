.PHONY: setup-nsc server-config creds run-server push-accounts \
       run-subscriber run-processor run-postprocessor run-publisher demo clean help

NSC_STORE := $(HOME)/.local/share/nats/nsc/stores
OPERATOR := msg
ACCOUNT  := PIPELINE
CREDS_DIR := creds

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

# ---------------------------------------------------------------------------
# Step 1: Bootstrap nsc operator / account / users
# ---------------------------------------------------------------------------
setup-nsc: ## Create operator, account, and users with subject permissions
	@echo "==> Initialising nsc operator '$(OPERATOR)' ..."
	nsc init --name $(OPERATOR) --dir $(NSC_STORE)
	@echo "==> Adding account '$(ACCOUNT)' ..."
	nsc add account --name $(ACCOUNT)
	@echo "==> Adding users ..."
	nsc add user --name publisher      --account $(ACCOUNT)
	nsc add user --name subscriber     --account $(ACCOUNT)
	nsc add user --name processor      --account $(ACCOUNT)
	nsc add user --name postprocessor  --account $(ACCOUNT)
	@echo "==> Setting subject permissions ..."
	nsc edit user --name publisher  --account $(ACCOUNT) \
		--allow-pub "msg.raw" \
		--allow-sub "_INBOX.>" \
		--deny-sub ">"
	nsc edit user --name subscriber --account $(ACCOUNT) \
		--allow-sub "msg.raw,msg.enhanced,msg.final,_INBOX.>" \
		--allow-pub "_INBOX.>" \
		--deny-pub ">"
	nsc edit user --name processor  --account $(ACCOUNT) \
		--allow-sub "msg.raw,_INBOX.>" \
		--allow-pub "msg.enhanced,_INBOX.>" \
		--deny-pub ">"
	nsc edit user --name postprocessor --account $(ACCOUNT) \
		--allow-sub "msg.enhanced,_INBOX.>" \
		--allow-pub "msg.final,_INBOX.>" \
		--deny-pub ">"
	@echo "==> Done. Run 'make server-config' next."

# ---------------------------------------------------------------------------
# Step 2: Generate nats-server config from nsc
# ---------------------------------------------------------------------------
server-config: ## Generate nats-server.conf with NATS account resolver
	nsc generate config --nats-resolver --config-file ./nats-server.conf
	mkdir -p nats-data
	@echo "==> nats-server.conf written. Run 'make creds' next."

# ---------------------------------------------------------------------------
# Step 3: Export .creds files
# ---------------------------------------------------------------------------
creds: $(CREDS_DIR)/publisher.creds $(CREDS_DIR)/subscriber.creds $(CREDS_DIR)/processor.creds $(CREDS_DIR)/postprocessor.creds ## Generate .creds files for all users
	@echo "==> Credentials ready in $(CREDS_DIR)/."

$(CREDS_DIR)/%.creds:
	@mkdir -p $(CREDS_DIR)
	nsc generate creds --account $(ACCOUNT) --name $* > $@

# ---------------------------------------------------------------------------
# Step 4–8: Run the pipeline
# ---------------------------------------------------------------------------
run-server: ## Start nats-server (foreground)
	nats-server -c ./nats-server.conf

push-accounts: ## Push account JWTs to running server
	nsc push --all --system-account SYS

run-subscriber: ## Run subscriber listening on msg.final
	go run ./cmd/subscriber --subject msg.final --creds $(CREDS_DIR)/subscriber.creds

run-processor: ## Run processor (msg.raw → msg.enhanced)
	go run ./cmd/processor --creds $(CREDS_DIR)/processor.creds

run-postprocessor: ## Run postprocessor (msg.enhanced → msg.final)
	go run ./cmd/postprocessor --creds $(CREDS_DIR)/postprocessor.creds

run-publisher: ## Publish 5 messages at 500ms intervals
	go run ./cmd/publisher --message "hello nats world" --count 5 --interval 500ms --creds $(CREDS_DIR)/publisher.creds

# ---------------------------------------------------------------------------
# Convenience
# ---------------------------------------------------------------------------
demo: ## Print instructions for running the full pipeline
	@echo ""
	@echo "Open 5 terminals and run these commands in order:"
	@echo ""
	@echo "  Terminal 1:  make run-server"
	@echo "  Terminal 2:  make push-accounts   (wait for server to start, then)"
	@echo "               make run-subscriber"
	@echo "  Terminal 3:  make run-processor"
	@echo "  Terminal 4:  make run-postprocessor"
	@echo "  Terminal 5:  make run-publisher"
	@echo ""
	@echo "The subscriber should print 5 messages with both processor and postprocessor metadata."
	@echo ""

clean: ## Remove generated creds, data, config, and binaries
	rm -rf $(CREDS_DIR) nats-data nats-server.conf
	rm -f publisher subscriber processor postprocessor
	@echo "==> Cleaned."
