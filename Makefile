ARCH?=amd64
REPO?=#your repository here 
VERSION?=0.1

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=$(ARCH) go build -o ./bin/resource-tree-handler main.go

container:
	docker build -t $(REPO)resource-tree-handler:$(VERSION) .
	docker push $(REPO)resource-tree-handler:$(VERSION)

container-multi:
	docker buildx build --tag $(REPO)resource-tree-handler:$(VERSION) --push --platform linux/amd64,linux/arm64 .