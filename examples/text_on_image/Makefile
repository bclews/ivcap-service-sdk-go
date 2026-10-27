SERVICE=text-on-image
SERVICE_TITLE=Create an image with custom text
PROVIDER_NAME=testing.com

# WARNING!! Changing the service or provider name WILL change hte UUID (platform ONLY looks at UUID)
#SERVICE_ID=cayp:service:cayp-example-gradient-image
SERVICE_CONTAINER_NAME=${SERVICE}
SERVICE_ID=ivcap:service:$(shell python3 -c 'import uuid; print(uuid.uuid5(uuid.NAMESPACE_DNS, \
        "${PROVIDER_NAME}" + "${SERVICE_CONTAINER_NAME}"));'):${SERVICE_CONTAINER_NAME}

VERSION=latest
LOCAL_DOCKER_REGISTRY=localhost:5000
GKE_DOCKER_REGISTRY=australia-southeast1-docker.pkg.dev/reinvent-science-prod-2ae1/ivap-registry/cloud-build
DOCKER_REGISTRY=${LOCAL_DOCKER_REGISTRY}

DOCKER_NAME=$(shell echo ${SERVICE_CONTAINER_NAME} | sed -E 's/-/_/g')
DOCKER_TAG=$(shell echo ${PROVIDER_NAME} | sed -E 's/-/_/g')/${DOCKER_NAME}:${VERSION}
DOCKER_DEPLOY=${DOCKER_REGISTRY}/${DOCKER_TAG}

IMG_URL=https://juststickers.in/wp-content/uploads/2016/07/go-programming-language.png

GIT_COMMIT=$(shell git rev-parse --short HEAD)
LD_FLAGS := -X main.GitCommit=${GIT_COMMIT}
GIT_TAG=$(shell git describe --abbrev=0 --tags ${TAG_COMMIT} 2>/dev/null || true)
ifneq ($(GIT_TAG),)
LD_FLAGS := ${LD_FLAGS} -X main.GitTag=$(GIT_TAG)
endif
LD_FLAGS := ${LD_FLAGS} -X main.BuildDate=$(shell date "+%Y-%m-%d:%H:%M")
ROOT_DIR:=$(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))

build:
	@go mod tidy
	@cd ${ROOT_DIR} && go build -ldflags "${LD_FLAGS}" -o ${ROOT_DIR}/${SERVICE}

clean: 
	rm -f ${ROOT_DIR}/${SERVICE}

run-dev:
	env http_proxy=http://0.0.0.0:9999 \
		IVCAP_BASE_URL=http://ivcap.local \
		IVCAP_CACHE_URL=http://cache.local \
		${ROOT_DIR}/${SERVICE} ${RUN_DEV_ARGS}

run-dev-img-url:
	make RUN_DEV_ARGS="--msg foo --img-url ${IMG_URL}" run-dev

build-run-dev: build run-dev

dispatch-test-order:
	curl -XPOST http://localhost:8088/1/orders --data @${ROOT_DIR}/test-order.json

docker-build:
	@echo "Building docker image ${DOCKER_NAME}"
	docker build -t ${DOCKER_NAME} \
		--build-arg GIT_COMMIT=${GIT_COMMIT} \
		--build-arg GIT_TAG=${GIT_TAG} \
		--build-arg SERVICE_ID=${SERVICE_ID} \
		-f ${ROOT_DIR}/Dockerfile \
		$(shell realpath ${ROOT_DIR})/../.. ${DOCKER_BILD_ARGS}
	@echo "\nFinished building docker image ${DOCKER_NAME}\n"

docker-publish: docker-build
	@echo "====> If 'unauthorized: authentication required' log into ACR with 'az acr login --name cipmain'"
	docker tag ${DOCKER_NAME} ${DOCKER_DEPLOY}
	docker push ${DOCKER_DEPLOY}

service-create: #docker-publish
	sed -E 's|@CONTAINER@|${DOCKER_DEPLOY}|g' ${ROOT_DIR}/service.yaml \
	| sed  -E 's|@ACCOUNT_ID@|$(shell ivcap context get account-id)|g' \
	| ivcap service update --create ${SERVICE_ID} --format yaml -f - --timeout 600

service-get:
	ivcap service get ${SERVICE_ID}

service-list:
	curl -i -XGET \
			-H "Authorization: Bearer ${JWT}" \
			-H "Content-Type: application/json" \
			"${CRE_URL}/1/services?limit=2&offset=30"

test-order:
	ivcap order create  ${SERVICE_ID} -n "test: $(shell date)" msg="Hello ${USER}" img-url=${IMG_URL}

test-order-url:
	sed  -E 's|@SERVICE_ID@|${SERVICE_ID}|g' ${ROOT_DIR}/test-order-url.json \
	| sed  -E 's|@ACCOUNT_ID@|${ACCOUNT_ID}|g' \
	| curl -XPOST -i \
			-H "Authorization: Bearer ${JWT}" \
			-H "Content-Type: application/json" \
			--data @- \
			${CRE_URL}/1/orders

