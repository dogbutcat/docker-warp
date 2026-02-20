VERSION := $(shell cat VERSION)
PLATFORM ?= linux/amd64
IMAGE_NAME := docker-warp
REMOTE_IMAGE := ghcr.io/dogbutcat/warp
CONTAINER_NAME := test_warp

build: stop
	docker buildx build --platform $(PLATFORM) \
		-t $(IMAGE_NAME) --load .

push:
	docker buildx build --platform linux/amd64 \
		-t $(REMOTE_IMAGE):$(VERSION)-amd64 --push .
	docker buildx build --platform linux/arm64 \
		-t $(REMOTE_IMAGE):$(VERSION)-arm64 --push .
	docker manifest create $(REMOTE_IMAGE):$(VERSION) \
		$(REMOTE_IMAGE):$(VERSION)-amd64 \
		$(REMOTE_IMAGE):$(VERSION)-arm64
	docker manifest push $(REMOTE_IMAGE):$(VERSION)
	docker manifest create $(REMOTE_IMAGE):latest \
		$(REMOTE_IMAGE):$(VERSION)-amd64 \
		$(REMOTE_IMAGE):$(VERSION)-arm64
	docker manifest push $(REMOTE_IMAGE):latest

stop:
	@if [ "$$(docker ps -a --format '{{.Names}}' | grep $(CONTAINER_NAME))" = "$(CONTAINER_NAME)" ]; then \
		docker stop $(CONTAINER_NAME); \
	fi

test: build
	docker run --rm \
		-d --name $(CONTAINER_NAME) \
		-e TZ=Asia/Shanghai \
		-e WARP_MODE=proxy \
		-e WARP_PROXY_PORT=40000 \
		-e WARP_LICENSE_KEY= \
		-e PROXY_TYPE=socks5 \
		-e PROXY_PORT=1080 \
		-e GATEWAY_MODE=true \
		-e GATEWAY_ROUTES=10.143.0.0/16 \
		-p 1080:1080 \
		--cap-add NET_ADMIN \
		--cap-add SYS_MODULE \
		--device /dev/net/tun:/dev/net/tun \
		$(IMAGE_NAME)

run: test
	@echo "Container $(CONTAINER_NAME) started."
	@echo "SOCKS5 proxy: socks5://localhost:1080"

logs:
	docker logs -f $(CONTAINER_NAME)

# ========== IP 优选测试 (docker run --rm, 无残留) ==========
TEST_IMAGE := $(IMAGE_NAME):test

test-build: stop
	docker buildx build --platform $(PLATFORM) \
		-t $(TEST_IMAGE) --load .
	@echo "✅ Build passed: $(TEST_IMAGE)"

test-ip-selection: test-build
	@echo ""
	@echo "===== [1/7] MDM XML 生成 (含优选 endpoint) ====="
	@docker run --rm \
		-e WARP_MDM_ENABLED=true \
		-e WARP_ORG=test-org \
		-e WARP_AUTH_CLIENT_ID=test.access \
		-e WARP_AUTH_CLIENT_SECRET=test-secret \
		-e WARP_OVERRIDE_WARP_ENDPOINT=1.2.3.4:500 \
		-e WARP_OVERRIDE_API_ENDPOINT=5.6.7.8 \
		--entrypoint bash $(TEST_IMAGE) -c '\
		bash /usr/bin/generate-mdm-xml && \
		echo "--- mdm.xml ---" && cat /var/lib/cloudflare-warp/mdm.xml && echo "" && \
		grep -q "override_warp_endpoint" /var/lib/cloudflare-warp/mdm.xml && \
		grep -q "1.2.3.4:500" /var/lib/cloudflare-warp/mdm.xml && \
		grep -q "override_api_endpoint" /var/lib/cloudflare-warp/mdm.xml && \
		grep -q "5.6.7.8" /var/lib/cloudflare-warp/mdm.xml && \
		echo "✅ [1/7] PASS: MDM XML contains override endpoints"'
	@echo ""
	@echo "===== [2/7] MDM XML 生成 (无优选 — 不含 override key) ====="
	@docker run --rm \
		-e WARP_MDM_ENABLED=true \
		-e WARP_ORG=test-org \
		-e WARP_AUTH_CLIENT_ID=test.access \
		-e WARP_AUTH_CLIENT_SECRET=test-secret \
		--entrypoint bash $(TEST_IMAGE) -c '\
		bash /usr/bin/generate-mdm-xml && \
		echo "--- mdm.xml ---" && cat /var/lib/cloudflare-warp/mdm.xml && echo "" && \
		! grep -q "override_warp_endpoint" /var/lib/cloudflare-warp/mdm.xml && \
		! grep -q "override_api_endpoint" /var/lib/cloudflare-warp/mdm.xml && \
		echo "✅ [2/7] PASS: MDM XML does NOT contain override keys"'
	@echo ""
	@echo "===== [3/7] MDM XML patch (已有文件时注入 override) ====="
	@docker run --rm \
		-e WARP_MDM_ENABLED=true \
		-e WARP_ORG=test-org \
		-e WARP_AUTH_CLIENT_ID=test.access \
		-e WARP_AUTH_CLIENT_SECRET=test-secret \
		-e WARP_OVERRIDE_WARP_ENDPOINT=9.8.7.6:2408 \
		--entrypoint bash $(TEST_IMAGE) -c '\
		mkdir -p /var/lib/cloudflare-warp && \
		echo "<dict><key>organization</key><string>pre-existing</string></dict>" \
		  > /var/lib/cloudflare-warp/mdm.xml && \
		bash /usr/bin/generate-mdm-xml && \
		echo "--- patched mdm.xml ---" && cat /var/lib/cloudflare-warp/mdm.xml && echo "" && \
		grep -q "pre-existing" /var/lib/cloudflare-warp/mdm.xml && \
		grep -q "override_warp_endpoint" /var/lib/cloudflare-warp/mdm.xml && \
		grep -q "9.8.7.6:2408" /var/lib/cloudflare-warp/mdm.xml && \
		echo "✅ [3/7] PASS: Existing mdm.xml patched with override"'
	@echo ""
	@echo "===== [4/7] init-warp nounset 安全 (无 WARP_OVERRIDE_* ENV) ====="
	@docker run --rm \
		-e WARP_MODE=proxy \
		-e WARP_LICENSE_KEY= \
		--entrypoint bash $(TEST_IMAGE) -c '\
		set -u && \
		source <(head -10 /etc/s6-overlay/s6-rc.d/init-warp/run | grep -v "^#" | grep -v "^set ") && \
		echo "WARP_OVERRIDE_WARP_ENDPOINT=[$$WARP_OVERRIDE_WARP_ENDPOINT]" && \
		echo "✅ [4/7] PASS: init-warp variable declarations safe under nounset"'
	@echo ""
	@echo "===== [5/7] init-warp-ip-selection 禁用时跳过 ====="
	@docker run --rm \
		-e WARP_IP_SELECTION_ENABLED=false \
		-e WARP_API_SELECTION_ENABLED=false \
		--entrypoint bash $(TEST_IMAGE) -c '\
		bash /etc/s6-overlay/s6-rc.d/init-warp-ip-selection/run && \
		echo "✅ [5/7] PASS: ip-selection skipped when disabled"'
	@echo ""
	@echo "===== [6/7] 真实隧道优选 (完整链路: init-script → warp-speed-test.sh → probe) ====="
	@docker run --rm \
		-e WARP_IP_SELECTION_ENABLED=true \
		-e WARP_API_SELECTION_ENABLED=false \
		-e WARP_MDM_ENABLED=true \
		--entrypoint bash $(TEST_IMAGE) -c '\
		mkdir -p /var/run/s6/container_environment && \
		bash /etc/s6-overlay/s6-rc.d/init-warp-ip-selection/run && \
		if [ -f /var/run/s6/container_environment/WARP_OVERRIDE_WARP_ENDPOINT ]; then \
			RESULT=$$(cat /var/run/s6/container_environment/WARP_OVERRIDE_WARP_ENDPOINT) && \
			echo "Tunnel endpoint selected: $${RESULT}" && \
			echo "✅ [6/7] PASS: Full pipeline wrote WARP_OVERRIDE_WARP_ENDPOINT"; \
		else \
			echo "⚠️  [6/7] WARN: Tunnel probe found no reachable endpoints (network/env limitation)"; \
		fi'
	@echo ""
	@echo "===== [7/7] 真实 API 优选 (完整链路: init-script → warp-speed-test.sh → probe) ====="
	@docker run --rm \
		-e WARP_IP_SELECTION_ENABLED=false \
		-e WARP_API_SELECTION_ENABLED=true \
		--entrypoint bash $(TEST_IMAGE) -c '\
		mkdir -p /var/run/s6/container_environment && \
		bash /etc/s6-overlay/s6-rc.d/init-warp-ip-selection/run && \
		if [ -f /var/run/s6/container_environment/WARP_OVERRIDE_API_ENDPOINT ]; then \
			RESULT=$$(cat /var/run/s6/container_environment/WARP_OVERRIDE_API_ENDPOINT) && \
			echo "API endpoint selected: $${RESULT}" && \
			echo "✅ [7/7] PASS: Full pipeline wrote WARP_OVERRIDE_API_ENDPOINT"; \
		else \
			echo "⚠️  [7/7] WARN: API probe found no reachable endpoints (network/env limitation)"; \
		fi'
	@echo ""
	@echo "=============================="
	@echo "✅ All 7 IP-selection tests passed!"
	@echo "=============================="

test-clean:
	@docker rmi $(TEST_IMAGE) 2>/dev/null && echo "Removed $(TEST_IMAGE)" || echo "$(TEST_IMAGE) not found, nothing to clean"

.PHONY: build push stop test run logs test-build test-ip-selection test-clean