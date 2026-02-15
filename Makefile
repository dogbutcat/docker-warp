VERSION := $(shell cat VERSION)
PLATFORM ?= linux/amd64
IMAGE_NAME := docker-warp
REMOTE_IMAGE := dogbutcat/warp
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

.PHONY: build push stop test run logs