VERSION := $(shell cat VERSION)
PLATFORM ?= linux/amd64
IMAGE_NAME := docker-warp
REMOTE_IMAGE := dogbutcat/warp
CONTAINER_NAME := test_warp

HTTP_PORT ?= 3000
HTTPS_PORT ?= 3001
KCLIENT_PORT ?= 6910
WEBSOCKET_PORT ?= 6911

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
		-e WARP_PROXY_PORT=1080 \
		-e WARP_LICENSE_KEY= \
		-e CUSTOM_PORT=$(HTTP_PORT) \
		-p $(HTTP_PORT):$(HTTP_PORT) \
		--cap-add NET_ADMIN \
		--cap-add SYS_MODULE \
		--device /dev/net/tun:/dev/net/tun \
		-v warp-data:/var/lib/cloudflare-warp \
		--shm-size 1g \
		$(IMAGE_NAME)

run: test
	@echo "Container $(CONTAINER_NAME) started."
	@echo "Access KasmVNC at http://localhost:$(HTTP_PORT)"

logs:
	docker logs -f $(CONTAINER_NAME)

.PHONY: build push stop test run logs